package mailbox

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

type IssueExportInput struct {
	CardFilters []CardFilter `json:"card_filters"`
	Scope       string       `json:"scope"`
	AccountType string       `json:"account_type"`
	Search      string       `json:"search"`
	Status      string       `json:"status"`
	Kind        string       `json:"kind"`
	OperatorID  int64        `json:"operator_id"`
}

// Only selected, allowlisted columns enter a report. Payment secrets are never queried.
type dataExportRow struct {
	ID, AccountID, AssignmentID, SubmissionID, OperatorID, AdminID             int64
	Version, AccountVersion, AssignmentVersion, OperatorVersion                int64
	CreatedAt, UpdatedAt, ReviewedAt, ResolvedAt, RevokedAt, ArchivedAt        int64
	AttachmentCount                                                            int64
	Active                                                                     bool
	AccountType, Email, OperatorName, AdminName, Status, Kind                  string
	Remark, ReviewReason, Description, Resolution, Reply, OldRemark, NewRemark string
	Ciphertext, KeyVersion                                                     string
}

type dataExportSheet struct {
	name    string
	headers []string
	query   func() *gorm.DB
	key     string
	cells   func(dataExportRow) ([]string, error)
}

func exportTime(value int64) string {
	if value == 0 {
		return ""
	}
	return time.Unix(value, 0).In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")
}

func exportLabel(value string) string {
	labels := map[string]string{"refund": "退款", "opening": "开号", "unassigned": "未分配", "pending": "待处理", "submitted": "待审核", "approved": "已通过", "rejected": "已退回", "issue_pending": "异常待处理", "resolved": "已处理", "invalidated": "分配已失效", "email_login": "邮箱无法登录", "otp": "验证码异常", "card": "信用卡不可用", "other": "其他", "resume": "恢复任务", "recall": "回收"}
	if label, ok := labels[value]; ok {
		return label
	}
	return value
}

func exportBool(value bool) string {
	if value {
		return "是"
	}
	return "否"
}
func exportID(value int64) string {
	if value == 0 {
		return ""
	}
	return fmt.Sprint(value)
}

func (s *Service) loginExportSheet(name string, accountIDs func() *gorm.DB) dataExportSheet {
	return dataExportSheet{name: name, key: "a.id", headers: []string{"邮箱 ID", "邮箱类型", "邮箱", "密码", "2FA 密钥", "2FA 算法", "2FA 位数", "2FA 周期秒", "当前状态", "当前操作员 ID", "当前操作员", "已归档", "创建时间（北京时间）"},
		query: func() *gorm.DB {
			q := s.DB.Table("mailbox_accounts AS a").Joins("LEFT JOIN mailbox_assignments AS t ON t.id = a.active_assignment_id AND t.account_id = a.id AND t.revoked_at = 0").Joins("LEFT JOIN mailbox_operators AS o ON o.id = t.operator_id")
			if accountIDs != nil {
				q = q.Where("a.id IN (?)", accountIDs())
			}
			return q.Select("a.id, a.version, a.account_type, a.email, a.ciphertext, a.key_version, a.archived_at, a.created_at, COALESCE(t.status, 'unassigned') AS status, COALESCE(t.id, 0) AS assignment_id, COALESCE(t.version, 0) AS assignment_version, COALESCE(o.id, 0) AS operator_id, COALESCE(o.version, 0) AS operator_version, COALESCE(o.display_name, '') AS operator_name")
		}, cells: func(row dataExportRow) ([]string, error) {
			cipher, err := s.Cipher()
			if err != nil {
				return nil, fail(503, "mailbox_credentials_unavailable")
			}
			payload, err := cipher.Decrypt(row.ID, mailboxCredentialKind, row.KeyVersion, row.Ciphertext)
			if err != nil {
				return nil, fail(503, "mailbox_credentials_unavailable")
			}
			var secret mailboxCredentialSecret
			if json.Unmarshal([]byte(payload.Secret), &secret) != nil {
				return nil, fail(503, "mailbox_credentials_unavailable")
			}
			key, algorithm, digits, period := mailboxOTPAbsentMarker, "", "", ""
			if !secret.OTPAbsent {
				if _, err := secret.OTP.code(s.Now()); err != nil {
					return nil, err
				}
				key, algorithm, digits, period = secret.OTP.Secret, secret.OTP.Algorithm.String(), fmt.Sprint(int(secret.OTP.Digits)), fmt.Sprint(secret.OTP.Period)
			}
			return []string{exportID(row.ID), exportLabel(row.AccountType), row.Email, secret.Password, key, algorithm, digits, period, exportLabel(row.Status), exportID(row.OperatorID), row.OperatorName, exportBool(row.ArchivedAt != 0), exportTime(row.CreatedAt)}, nil
		}}
}

func (s *Service) issueExportSheet(query func() *gorm.DB) dataExportSheet {
	return dataExportSheet{name: "异常反馈", key: "i.id", headers: []string{"反馈 ID", "邮箱 ID", "分配 ID", "邮箱类型", "邮箱", "操作员 ID", "操作员", "问题类型", "说明", "处理状态", "处理方式", "管理员 ID", "管理员", "管理员回复", "提交时间（北京时间）", "处理时间（北京时间）", "分配仍有效", "截图数量"},
		query: func() *gorm.DB {
			return query().Joins("LEFT JOIN users AS admin ON admin.id = i.resolved_by").Select("i.id, i.version, i.account_id, i.assignment_id, a.account_type, a.email, a.version AS account_version, t.version AS assignment_version, o.version AS operator_version, i.operator_id, o.display_name AS operator_name, i.kind, i.description, i.status, i.resolution, i.resolved_by AS admin_id, COALESCE(admin.username, '') AS admin_name, i.reply, i.created_at, i.resolved_at, CASE WHEN a.active_assignment_id = t.id AND t.revoked_at = 0 AND a.archived_at = 0 AND o.enabled = true THEN true ELSE false END AS active, (SELECT COUNT(*) FROM mailbox_attachments AS f WHERE f.issue_id = i.id AND f.submission_id = 0 AND f.assignment_id = i.assignment_id AND f.operator_id = i.operator_id) AS attachment_count")
		},
		cells: func(r dataExportRow) ([]string, error) {
			status := exportLabel(r.Status)
			if r.Status == "pending" {
				status = "待处理"
			}
			return []string{exportID(r.ID), exportID(r.AccountID), exportID(r.AssignmentID), exportLabel(r.AccountType), r.Email, exportID(r.OperatorID), r.OperatorName, exportLabel(r.Kind), r.Description, status, exportLabel(r.Resolution), exportID(r.AdminID), r.AdminName, r.Reply, exportTime(r.CreatedAt), exportTime(r.ResolvedAt), exportBool(r.Active), fmt.Sprint(r.AttachmentCount)}, nil
		}}
}

func (s *Service) historyExportSheets() []dataExportSheet {
	return []dataExportSheet{
		{name: "分配历史", key: "t.id", headers: []string{"分配 ID", "邮箱 ID", "邮箱类型", "邮箱", "操作员 ID", "操作员", "状态", "分配仍有效", "分配管理员 ID", "分配时间（北京时间）", "回收时间（北京时间）", "更新时间（北京时间）"},
			query: func() *gorm.DB {
				return s.DB.Table("mailbox_assignments AS t").Joins("JOIN mailbox_accounts AS a ON a.id = t.account_id").Joins("JOIN mailbox_operators AS o ON o.id = t.operator_id").Select("t.id, t.version, t.account_id, a.account_type, a.email, a.version AS account_version, t.operator_id, o.version AS operator_version, o.display_name AS operator_name, t.status, t.assigned_by AS admin_id, t.created_at, t.revoked_at, t.updated_at, CASE WHEN a.active_assignment_id = t.id AND t.revoked_at = 0 AND a.archived_at = 0 AND o.enabled = true THEN true ELSE false END AS active")
			},
			cells: func(r dataExportRow) ([]string, error) {
				return []string{exportID(r.ID), exportID(r.AccountID), exportLabel(r.AccountType), r.Email, exportID(r.OperatorID), r.OperatorName, exportLabel(r.Status), exportBool(r.Active), exportID(r.AdminID), exportTime(r.CreatedAt), exportTime(r.RevokedAt), exportTime(r.UpdatedAt)}, nil
			}},
		{name: "提交审核", key: "u.id", headers: []string{"提交 ID", "邮箱 ID", "分配 ID", "邮箱类型", "邮箱", "操作员 ID", "操作员", "提交状态", "最新备注", "审核结论", "审核原因", "审核管理员 ID", "提交时间（北京时间）", "审核时间（北京时间）", "截图数量"},
			query: func() *gorm.DB {
				return s.DB.Table("mailbox_submissions AS u").Joins("JOIN mailbox_assignments AS t ON t.id = u.assignment_id AND t.operator_id = u.operator_id").Joins("JOIN mailbox_accounts AS a ON a.id = t.account_id").Joins("JOIN mailbox_operators AS o ON o.id = u.operator_id").Select("u.id, u.version, a.id AS account_id, a.version AS account_version, t.id AS assignment_id, t.version AS assignment_version, a.account_type, a.email, u.operator_id, o.version AS operator_version, o.display_name AS operator_name, u.status, u.remark, u.review_reason, u.reviewed_by AS admin_id, u.created_at, u.reviewed_at, (SELECT COUNT(*) FROM mailbox_attachments AS f WHERE f.submission_id = u.id AND f.issue_id = 0 AND f.assignment_id = u.assignment_id AND f.operator_id = u.operator_id) AS attachment_count")
			},
			cells: func(r dataExportRow) ([]string, error) {
				status := exportLabel(r.Status)
				verdict := ""
				if r.Status == StatusPending {
					status = "待审核"
				}
				if r.ReviewedAt != 0 {
					verdict = status
				}
				return []string{exportID(r.ID), exportID(r.AccountID), exportID(r.AssignmentID), exportLabel(r.AccountType), r.Email, exportID(r.OperatorID), r.OperatorName, status, r.Remark, verdict, r.ReviewReason, exportID(r.AdminID), exportTime(r.CreatedAt), exportTime(r.ReviewedAt), fmt.Sprint(r.AttachmentCount)}, nil
			}},
		{name: "备注修订", key: "r.id", headers: []string{"修订 ID", "邮箱 ID", "分配 ID", "提交 ID", "邮箱类型", "邮箱", "原备注", "新备注", "修改管理员 ID", "修改管理员", "修改操作员 ID", "修改操作员", "版本", "修改时间（北京时间）"},
			query: func() *gorm.DB {
				return s.DB.Table("mailbox_remark_revisions AS r").Joins("JOIN mailbox_submissions AS u ON u.id = r.submission_id").Joins("JOIN mailbox_assignments AS t ON t.id = u.assignment_id AND t.operator_id = u.operator_id").Joins("JOIN mailbox_accounts AS a ON a.id = t.account_id").Joins("LEFT JOIN mailbox_operators AS o ON o.id = r.operator_id").Joins("LEFT JOIN users AS admin ON admin.id = r.admin_id").Select("r.id, r.version, a.id AS account_id, a.version AS account_version, t.id AS assignment_id, t.version AS assignment_version, u.id AS submission_id, a.account_type, a.email, r.old_remark, r.new_remark, r.admin_id, COALESCE(admin.username, '') AS admin_name, r.operator_id, COALESCE(o.display_name, '') AS operator_name, r.created_at")
			},
			cells: func(r dataExportRow) ([]string, error) {
				return []string{exportID(r.ID), exportID(r.AccountID), exportID(r.AssignmentID), exportID(r.SubmissionID), exportLabel(r.AccountType), r.Email, r.OldRemark, r.NewRemark, exportID(r.AdminID), r.AdminName, exportID(r.OperatorID), r.OperatorName, fmt.Sprint(r.Version), exportTime(r.CreatedAt)}, nil
			}},
	}
}

// Keyset pagination and a second fingerprint pass detect additions, edits and removals.
// Fingerprints include selected versions and display fields, never plaintext credentials.
func readExportSheet(spec dataExportSheet, consume func(dataExportRow) error) ([32]byte, int, error) {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	var last int64
	count := 0
	for {
		var rows []dataExportRow
		if err := spec.query().Where(spec.key+" > ?", last).Order(spec.key).Limit(250).Scan(&rows).Error; err != nil {
			return [32]byte{}, 0, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			count++
			if count > completedExportLimit {
				return [32]byte{}, 0, fail(400, "mailbox_export_limit")
			}
			if err := encoder.Encode(row); err != nil {
				return [32]byte{}, 0, err
			}
			if consume != nil {
				if err := consume(row); err != nil {
					return [32]byte{}, 0, err
				}
			}
			last = row.ID
		}
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest, count, nil
}

func (s *Service) writeDataExport(actor Actor, specs []dataExportSheet) ([]byte, map[string]int, error) {
	if err := s.CheckCompletedExport(actor); err != nil {
		return nil, nil, err
	}
	for _, spec := range specs {
		var count int64
		if err := spec.query().Count(&count).Error; err != nil {
			return nil, nil, err
		}
		if count > completedExportLimit {
			return nil, nil, fail(400, "mailbox_export_limit")
		}
	}
	f := excelize.NewFile()
	defer f.Close()
	counts := map[string]int{}
	hashes := make([][32]byte, len(specs))
	total := 0
	style, err := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}, NumFmt: 49})
	if err != nil {
		return nil, nil, err
	}
	for i, spec := range specs {
		if i == 0 {
			err = f.SetSheetName("Sheet1", spec.name)
		} else {
			_, err = f.NewSheet(spec.name)
		}
		if err != nil {
			return nil, nil, err
		}
		put := func(row int, cells []string) error {
			for col, value := range cells {
				cell, _ := excelize.CoordinatesToCellName(col+1, row)
				if err := f.SetCellStr(spec.name, cell, value); err != nil {
					return err
				}
			}
			return nil
		}
		if err := put(1, spec.headers); err != nil {
			return nil, nil, err
		}
		index := 1
		digest, count, err := readExportSheet(spec, func(row dataExportRow) error {
			if (index-1)%250 == 0 {
				if err := s.CheckCompletedExport(actor); err != nil {
					return err
				}
			}
			cells, err := spec.cells(row)
			if err != nil {
				return err
			}
			index++
			return put(index, cells)
		})
		if err != nil {
			return nil, nil, err
		}
		hashes[i] = digest
		counts[spec.name] = count
		total += count
		last, _ := excelize.ColumnNumberToName(len(spec.headers))
		area := fmt.Sprintf("A1:%s%d", last, count+1)
		for _, err := range []error{f.SetCellStyle(spec.name, "A1", fmt.Sprintf("%s%d", last, count+1), style), f.SetColWidth(spec.name, "A", last, 28), f.SetPanes(spec.name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}), f.AutoFilter(spec.name, area, nil)} {
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if total == 0 {
		return nil, nil, fail(400, "mailbox_export_empty")
	}
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, nil, err
	}
	for i, spec := range specs {
		digest, count, err := readExportSheet(spec, nil)
		if err != nil {
			return nil, nil, err
		}
		if digest != hashes[i] || count != counts[spec.name] {
			return nil, nil, fail(409, "mailbox_export_changed")
		}
	}
	if err := s.CheckCompletedExport(actor); err != nil {
		return nil, nil, err
	}
	return buffer.Bytes(), counts, nil
}

type AccountExportInput struct {
	Scope string `json:"scope"`
	ListQuery
}

func (s *Service) ExportAllData(ctx context.Context, actor Actor, inputs ...AccountExportInput) ([]byte, map[string]int, error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckCompletedExport(actor); err != nil {
		return nil, nil, err
	}
	specs := []dataExportSheet{s.loginExportSheet("邮箱资料", nil)}
	history := s.historyExportSheets()
	specs = append(specs, history[0], history[1], s.issueExportSheet(func() *gorm.DB { return s.issueBaseQuery(actor) }), history[2])
	if len(inputs) > 0 && inputs[0].Scope == "filtered" {
		var err error
		s, err = s.withAccountType(inputs[0].AccountType)
		if err != nil {
			return nil, nil, err
		}
		q, err := s.filteredAccountQuery(actor, inputs[0].ListQuery)
		if err != nil {
			return nil, nil, err
		}
		for i := range specs {
			original := specs[i].query
			specs[i].query = func() *gorm.DB { return original().Where("a.id IN (?)", q.Session(&gorm.Session{}).Select("a.id")) }
		}
	} else if len(inputs) > 0 && inputs[0].Scope != "" && inputs[0].Scope != "all" {
		return nil, nil, fail(400, "mailbox_invalid_query")
	}
	return s.writeDataExport(actor, specs)
}

func (s *Service) ExportIssues(ctx context.Context, actor Actor, input IssueExportInput) ([]byte, map[string]int, error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckCompletedExport(actor); err != nil {
		return nil, nil, err
	}
	var query func() *gorm.DB
	switch input.Scope {
	case "all":
		if input.AccountType != "" || input.Search != "" || input.Status != "" || input.Kind != "" || input.OperatorID != 0 || len(input.CardFilters) > 0 {
			return nil, nil, fail(400, "mailbox_invalid_query")
		}
		query = func() *gorm.DB { return s.issueBaseQuery(actor) }
	case "filtered":
		var err error
		s, err = s.withAccountType(input.AccountType)
		if err != nil {
			return nil, nil, err
		}
		q := ListQuery{Search: input.Search, Status: input.Status, Kind: input.Kind, OperatorID: input.OperatorID, CardFilters: input.CardFilters}
		filtered, err := s.filteredIssueQuery(actor, q)
		if err != nil {
			return nil, nil, err
		}
		query = func() *gorm.DB { return filtered.Session(&gorm.Session{}) }
	default:
		return nil, nil, fail(400, "mailbox_invalid_query")
	}
	return s.writeDataExport(actor, []dataExportSheet{s.issueExportSheet(query), s.loginExportSheet("关联邮箱资料", func() *gorm.DB { return query().Select("i.account_id") })})
}
