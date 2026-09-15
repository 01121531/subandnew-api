package mailbox

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/mail"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const mailboxImportMaxRows = 1000
const mailboxImportMaxBytes = 10 << 20
const mailboxImportUnzipBytes = 32 << 20

type ImportPreviewRow struct {
	AccountType string `json:"account_type"`
	CardLast4   string `json:"card_last4"`
	Row         int    `json:"row"`
	Email       string `json:"email"`
}

type ImportIssue struct {
	Row  int    `json:"row"`
	Code string `json:"code"`
}

type ImportPreview struct {
	Rows   []ImportPreviewRow `json:"rows"`
	Issues []ImportIssue      `json:"issues"`
	Total  int                `json:"total"`
	Valid  bool               `json:"valid"`
}

type ImportResult struct {
	Imported int `json:"imported"`
}

type mailboxImportRow struct {
	email  string
	secret mailboxCredentialSecret
	card   mailboxCardSecret
	cvv    string
}

func (s *Service) checkMailboxImportActor(actor Actor) error {
	if err := s.CheckActor(actor, authz.MailboxManage); err != nil {
		return err
	}
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	return nil
}

func (s *Service) PreviewImport(ctx context.Context, actor Actor, format string, data []byte, accountTypes ...string) (preview *ImportPreview, err error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	defer func() { s.auditMailboxFailure(actor, "import_preview", 0, err) }()
	scoped, err := s.withAccountType("", accountTypes...)
	if err != nil {
		return nil, err
	}
	s = scoped
	if err := s.checkMailboxImportActor(actor); err != nil {
		return nil, err
	}
	var rows []mailboxImportRow
	preview, rows, err = s.prepareMailboxImport(format, data)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.cvv != "" {
			if err := s.CheckActor(actor, authz.MailboxCredentials); err != nil {
				return nil, err
			}
			break
		}
	}
	if err := s.checkMailboxImportActor(actor); err != nil {
		return nil, err
	}
	status, code := 200, ""
	if !preview.Valid {
		status, code = 400, "mailbox_import_invalid"
	}
	if err := s.Audit(actor, "import_preview", 0, 0, status, code); err != nil {
		return nil, fail(500, "mailbox_service_unavailable")
	}
	return preview, nil
}

func (s *Service) Import(ctx context.Context, actor Actor, format string, data []byte, accountTypes ...string) (result *ImportResult, err error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	defer func() { s.auditMailboxFailure(actor, "import", 0, err) }()
	scoped, err := s.withAccountType("", accountTypes...)
	if err != nil {
		return nil, err
	}
	s = scoped
	if err := s.checkMailboxImportActor(actor); err != nil {
		return nil, err
	}
	preview, rows, err := s.prepareMailboxImport(format, data)
	if err != nil {
		return nil, err
	}
	if !preview.Valid {
		for _, issue := range preview.Issues {
			if issue.Code == "mailbox_import_archived" {
				return nil, fail(409, "mailbox_import_archived")
			}
		}
		return nil, fail(400, "mailbox_import_invalid")
	}
	cipher, err := s.Cipher()
	if err != nil {
		return nil, fail(503, "mailbox_credentials_unavailable")
	}
	cvvCount := 0
	for _, row := range rows {
		if row.cvv != "" {
			cvvCount++
		}
	}
	var cvvs *cvvStore
	if cvvCount > 0 {
		if err := s.CheckActor(actor, authz.MailboxCredentials); err != nil {
			return nil, err
		}
		cvvs = s.cvvStore()
		cvvs.mu.Lock()
		defer cvvs.mu.Unlock()
		cvvs.purgeLocked(s.Now())
		if len(cvvs.items)+cvvCount > temporaryCVVCapacity {
			return nil, fail(503, "mailbox_cvv_capacity")
		}
	}
	pendingCVVs := make(map[int64]string)
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		service := s.WithDB(tx)
		if err := service.checkMailboxImportActor(actor); err != nil {
			return err
		}
		now := s.Now().Unix()
		for _, row := range rows {
			account := model.MailboxAccount{AccountType: s.pool(), Email: row.email, Version: 1, CreatedBy: actor.Admin.UserID, CreatedAt: now, UpdatedAt: now}
			// Reserve the numeric ID inside the transaction before binding ciphertext to it.
			insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account)
			if insert.Error != nil {
				return fail(500, "mailbox_service_unavailable")
			}
			if insert.RowsAffected != 1 {
				var existing model.MailboxAccount
				if err := tx.Select("archived_at").Where("account_type = ? AND email = ?", s.pool(), row.email).First(&existing).Error; err == nil && existing.ArchivedAt != 0 {
					return fail(409, "mailbox_import_archived")
				}
				return fail(409, "mailbox_import_conflict")
			}
			encoded, err := json.Marshal(row.secret)
			if err != nil {
				return fail(500, "mailbox_service_unavailable")
			}
			ciphertext, version, _, err := cipher.Encrypt(account.ID, mailboxCredentialKind, managedinstance.CredentialPayload{Secret: string(encoded)})
			if err != nil {
				return fail(503, "mailbox_credentials_unavailable")
			}
			updates := map[string]interface{}{"ciphertext": ciphertext, "key_version": version}
			if s.pool() == AccountTypeOpening {
				encodedCard, err := json.Marshal(row.card)
				if err != nil {
					return fail(500, "mailbox_service_unavailable")
				}
				cardCiphertext, cardVersion, _, err := cipher.Encrypt(account.ID, mailboxCardCredentialKind, managedinstance.CredentialPayload{Secret: string(encodedCard)})
				if err != nil {
					return fail(503, "mailbox_credentials_unavailable")
				}
				updates["card_ciphertext"] = cardCiphertext
				updates["card_key_version"] = cardVersion
				updates["card_last4"] = row.card.Number[len(row.card.Number)-4:]
			}
			update := tx.Model(&model.MailboxAccount{}).Where("id = ? AND version = ?", account.ID, account.Version).Updates(updates)
			if update.Error != nil || update.RowsAffected != 1 {
				return fail(500, "mailbox_service_unavailable")
			}
			if row.cvv != "" {
				pendingCVVs[account.ID] = row.cvv
			}
		}
		if err := service.checkMailboxImportActor(actor); err != nil {
			return err
		}
		if cvvCount > 0 {
			if err := service.CheckActor(actor, authz.MailboxCredentials); err != nil {
				return err
			}
		}
		if err := service.Audit(actor, "import", 0, 0, 200, ""); err != nil {
			return fail(500, "mailbox_service_unavailable")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Start the handoff lifetime only once the entire import has committed.
	if cvvs != nil {
		now := s.Now()
		for id, value := range pendingCVVs {
			cvvs.putLocked(id, value, now, 0, 0, 0)
		}
	}
	return &ImportResult{Imported: len(rows)}, nil
}

func (s *Service) prepareMailboxImport(format string, data []byte) (*ImportPreview, []mailboxImportRow, error) {
	preview := &ImportPreview{Rows: []ImportPreviewRow{}, Issues: []ImportIssue{}}
	rows := []mailboxImportRow{}
	seen := map[string]int{}
	columns := 3
	if s.pool() == AccountTypeOpening {
		columns = 5
	}
	addIssue := func(row int, code string) { preview.Issues = append(preview.Issues, ImportIssue{Row: row, Code: code}) }
	headerColumns := 0
	consume := func(row int, cells []string, header bool) error {
		if header && mailboxImportHeader(cells, s.pool()) {
			headerColumns = len(cells)
			if headerColumns == 6 && !temporaryCVVEnabled() {
				addIssue(row, "mailbox_cvv_disabled")
			}
			return nil
		}
		preview.Total++
		if preview.Total > mailboxImportMaxRows {
			return fail(400, "mailbox_import_too_many_rows")
		}
		withCVV := columns == 5 && len(cells) == 6
		if (len(cells) != columns && !withCVV) || (headerColumns != 0 && len(cells) != headerColumns) {
			addIssue(row, "mailbox_import_columns")
			return nil
		}
		cvv := ""
		if withCVV {
			if !temporaryCVVEnabled() {
				addIssue(row, "mailbox_cvv_disabled")
				return nil
			}
			if !cvvPattern.MatchString(cells[5]) {
				addIssue(row, "mailbox_invalid_cvv")
				return nil
			}
			cvv = cells[5]
		}
		email := strings.ToLower(strings.TrimSpace(cells[0]))
		address, err := mail.ParseAddress(email)
		if err != nil || !validMailboxText(email) || len(email) > 320 || address.Address != email || address.Name != "" || !strings.Contains(email, "@") {
			addIssue(row, "mailbox_import_invalid_email")
			return nil
		}
		// Only a validated email is ever included in the preview, even on invalid rows.
		preview.Rows = append(preview.Rows, ImportPreviewRow{Row: row, Email: email, AccountType: s.pool()})
		if original, exists := seen[email]; exists {
			addIssue(row, "mailbox_import_duplicate")
			if original != 0 {
				addIssue(original, "mailbox_import_duplicate")
				seen[email] = 0
			}
		} else {
			seen[email] = row
		}
		if cells[1] == "" || !validMailboxText(cells[1]) {
			addIssue(row, "mailbox_import_invalid_password")
			return nil
		}
		config, err := parseMailboxOTP(cells[2])
		if err != nil {
			addIssue(row, "mailbox_import_invalid_otp")
			return nil
		}
		var card mailboxCardSecret
		if columns == 5 {
			card.Number, err = normalizeMailboxPAN(cells[3])
			if err != nil {
				addIssue(row, "mailbox_import_invalid_card_number")
				return nil
			}
			card.Expiry, err = normalizeMailboxExpiry(cells[4], s.Now())
			if err != nil {
				addIssue(row, "mailbox_import_invalid_card_expiry")
				return nil
			}
			preview.Rows[len(preview.Rows)-1].CardLast4 = card.Number[len(card.Number)-4:]
		}
		rows = append(rows, mailboxImportRow{email: email, secret: mailboxCredentialSecret{Password: cells[1], OTP: config}, card: card, cvv: cvv})
		return nil
	}
	if len(data) > mailboxImportMaxBytes {
		return nil, nil, fail(413, "mailbox_import_too_large")
	}
	var err error
	switch format {
	case "text", "csv":
		if !validMailboxText(string(data)) {
			return nil, nil, fail(400, "mailbox_import_invalid_encoding")
		}
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if format == "text" {
			scanner := bufio.NewScanner(bytes.NewReader(data))
			scanner.Buffer(make([]byte, 4096), mailboxImportMaxBytes+1)
			for row := 1; scanner.Scan(); row++ {
				line := scanner.Text()
				if strings.TrimSpace(line) == "" {
					continue
				}
				delimiter := "----"
				if strings.Contains(line, "\t") {
					delimiter = "\t"
					if strings.Contains(line, "----") {
						// Mixed delimiters cannot identify password boundaries reliably.
						err = consume(row, nil, false)
						if err != nil {
							break
						}
						continue
					}
				}
				if err = consume(row, strings.Split(line, delimiter), false); err != nil {
					break
				}
			}
			if err == nil {
				err = scanner.Err()
			}
		} else {
			reader := csv.NewReader(bytes.NewReader(data))
			reader.FieldsPerRecord = -1
			first := true
			recordOffset, recordLine := int64(0), 1
			for {
				var cells []string
				cells, err = reader.Read()
				if errors.Is(err, io.EOF) {
					err = nil
					break
				}
				if err != nil {
					err = fail(400, "mailbox_import_invalid_csv")
					break
				}
				row, _ := reader.FieldPos(0)
				record := data[recordOffset:reader.InputOffset()]
				if len(cells) == columns || (columns == 5 && len(cells) == 6) {
					cells[1] = mailboxCSVPassword(record, recordLine, reader)
				}
				recordOffset = reader.InputOffset()
				recordLine += bytes.Count(record, []byte{'\n'})
				err = consume(row, cells, first)
				first = false
				if err != nil {
					break
				}
			}
		}
	case "xlsx":
		err = readMailboxXLSXRows(data, func(row int, cells []string, header bool, code string) error {
			if code != "" {
				preview.Total++
				if preview.Total > mailboxImportMaxRows {
					return fail(400, "mailbox_import_too_many_rows")
				}
				addIssue(row, code)
				return nil
			}
			return consume(row, cells, header)
		}, s.pool())
	default:
		return nil, nil, fail(400, "mailbox_import_invalid_format")
	}
	if err != nil {
		return nil, nil, err
	}
	if preview.Total == 0 {
		addIssue(0, "mailbox_import_empty")
	}
	// Query in bounded batches to stay below SQLite's bind parameter limit.
	emails := make([]string, 0, len(seen))
	for email := range seen {
		emails = append(emails, email)
	}
	for start := 0; start < len(emails); start += 200 {
		var existing []model.MailboxAccount
		if err := s.DB.Select("email", "archived_at").Where("account_type = ?", s.pool()).Where("LOWER(email) IN ?", emails[start:min(start+200, len(emails))]).Find(&existing).Error; err != nil {
			return nil, nil, fail(500, "mailbox_service_unavailable")
		}
		for _, account := range existing {
			for _, row := range preview.Rows {
				if row.Email == strings.ToLower(account.Email) {
					code := "mailbox_import_exists"
					if account.ArchivedAt != 0 {
						code = "mailbox_import_archived"
					}
					addIssue(row.Row, code)
				}
			}
		}
	}
	preview.Valid = len(preview.Issues) == 0
	return preview, rows, nil
}

// encoding/csv validates quoting, but normalizes embedded CRLF. Its field
// positions let us recover the original password bytes after validation.
func mailboxCSVPassword(record []byte, recordLine int, reader *csv.Reader) string {
	fieldOffset := func(field int) int {
		line, column := reader.FieldPos(field)
		offset := 0
		for current := recordLine; current < line; current++ {
			offset += bytes.IndexByte(record[offset:], '\n') + 1
		}
		return offset + column - 1
	}
	password := string(record[fieldOffset(1) : fieldOffset(2)-1])
	if strings.HasPrefix(password, "\"") {
		return strings.ReplaceAll(password[1:len(password)-1], "\"\"", "\"")
	}
	return password
}

func mailboxImportHeader(cells []string, accountTypes ...string) bool {
	kind, err := resolveAccountType("", accountTypes...)
	if err != nil {
		return false
	}
	columns := 3
	if kind == AccountTypeOpening {
		columns = 5
	}
	if len(cells) != columns && !(columns == 5 && len(cells) == 6) {
		return false
	}
	aliases := [][]string{
		{"email", "\u90ae\u7bb1", "\u90ae\u7bb1\u5730\u5740", "\u7535\u5b50\u90ae\u7bb1"},
		{"password", "\u5bc6\u7801"},
		{"2fa", "totp", "\u4e8c\u6b65\u9a8c\u8bc1", "\u4e24\u6b65\u9a8c\u8bc1", "\u53cc\u91cd\u9a8c\u8bc1", "\u4e8c\u6b65\u9a8c\u8bc1\u5bc6\u94a5"},
		{"pan", "card_number", "\u5361\u53f7"},
		{"expiry", "card_expiry", "\u6709\u6548\u671f"},
		{"cvv"},
	}
	for i, cell := range cells {
		matched := false
		for _, alias := range aliases[i] {
			if strings.ToLower(strings.TrimSpace(cell)) == alias {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func readMailboxXLSX(data []byte, consume func(int, []string, bool) error, accountTypes ...string) error {
	kind, err := resolveAccountType("", accountTypes...)
	if err != nil {
		return err
	}
	return readMailboxXLSXRows(data, func(row int, cells []string, header bool, code string) error {
		if code != "" {
			return fail(400, code)
		}
		return consume(row, cells, header)
	}, kind)
}

func readMailboxXLSXRows(data []byte, consume func(int, []string, bool, string) error, kind string) error {
	invalid := func() error { return fail(400, "mailbox_import_invalid_xlsx") }
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return invalid()
	}
	var total uint64
	for _, entry := range archive.File {
		if entry.UncompressedSize64 > mailboxImportUnzipBytes || total > mailboxImportUnzipBytes-entry.UncompressedSize64 {
			return fail(413, "mailbox_import_too_large")
		}
		total += entry.UncompressedSize64
		if !strings.HasPrefix(entry.Name, "xl/worksheets/") || !strings.HasSuffix(entry.Name, ".xml") {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return invalid()
		}
		// Inspect XML, not displayed values: shared/empty formulas may have cached text.
		decoder := xml.NewDecoder(io.LimitReader(reader, mailboxImportUnzipBytes+1))
		for {
			token, tokenErr := decoder.Token()
			if errors.Is(tokenErr, io.EOF) {
				break
			}
			if tokenErr != nil {
				_ = reader.Close()
				return invalid()
			}
			if element, ok := token.(xml.StartElement); ok && element.Name.Local == "f" {
				_ = reader.Close()
				return fail(400, "mailbox_import_formula")
			}
		}
		if reader.Close() != nil {
			return invalid()
		}
	}
	book, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true, UnzipSizeLimit: mailboxImportUnzipBytes, UnzipXMLSizeLimit: mailboxImportUnzipBytes})
	if err != nil {
		return invalid()
	}
	defer book.Close()
	sheets := book.GetSheetList()
	if len(sheets) != 1 {
		return fail(400, "mailbox_import_ambiguous_sheet")
	}
	iterator, err := book.Rows(sheets[0])
	if err != nil {
		return invalid()
	}
	defer iterator.Close()
	first := true
	for row := 1; iterator.Next(); row++ {
		if row > mailboxImportMaxRows+1 {
			return fail(400, "mailbox_import_too_many_rows")
		}
		cells, err := iterator.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return invalid()
		}
		if len(cells) == 0 {
			continue
		}
		rowCode := ""
		if kind == AccountTypeOpening && len(cells) >= 4 && !(first && mailboxImportHeader(cells, kind)) {
			cell, _ := excelize.CoordinatesToCellName(4, row)
			cellType, err := book.GetCellType(sheets[0], cell)
			if err != nil {
				return invalid()
			}
			if cellType != excelize.CellTypeSharedString && cellType != excelize.CellTypeInlineString {
				rowCode = "mailbox_import_card_number_text_required"
				cells = nil
			}
		}
		if kind == AccountTypeOpening && len(cells) == 6 && !(first && mailboxImportHeader(cells, kind)) {
			cell, _ := excelize.CoordinatesToCellName(6, row)
			cellType, err := book.GetCellType(sheets[0], cell)
			if err != nil {
				return invalid()
			}
			if cellType != excelize.CellTypeSharedString && cellType != excelize.CellTypeInlineString {
				rowCode = "mailbox_import_cvv_text_required"
				cells = nil
			}
		}
		if err := consume(row, cells, first, rowCode); err != nil {
			return err
		}
		first = false
	}
	if iterator.Error() != nil {
		return invalid()
	}
	return nil
}
