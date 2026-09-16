package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/01121531/subandnew-api/service/authz"
	"github.com/pquerna/otp"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const completedExportLimit = 10000

type completedExportVersion struct {
	ID                int64
	AccountVersion    int64
	AssignmentID      int64
	AssignmentVersion int64
	SubmissionID      int64
	SubmissionVersion int64
}

type completedExportRow struct {
	Version                                                                   completedExportVersion `gorm:"embedded"`
	Email, Ciphertext, KeyVersion, OperatorName, Status, Remark, ReviewReason string
	CreatedAt, ReviewedAt                                                     int64
}

func (s *Service) CheckCompletedExport(actor Actor) error {
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	for _, permission := range []authz.Permission{authz.MailboxView, authz.MailboxReview, authz.MailboxCredentials} {
		if err := s.CheckActor(actor, permission); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) completedExportQuery() *gorm.DB {
	return s.DB.Table("mailbox_accounts AS a").
		Joins("JOIN mailbox_assignments AS t ON t.id = a.active_assignment_id AND t.account_id = a.id").
		Joins("JOIN mailbox_submissions AS u ON u.assignment_id = t.id AND u.operator_id = t.operator_id").
		Joins("JOIN mailbox_operators AS o ON o.id = t.operator_id").
		Where("a.account_type = ? AND a.archived_at = 0 AND t.revoked_at = 0 AND t.status IN ?", AccountTypeOpening, []string{StatusSubmitted, StatusApproved}).
		Where("u.id = (SELECT MAX(latest.id) FROM mailbox_submissions AS latest WHERE latest.assignment_id = t.id)")
}

const completedVersionSelect = "a.id, a.version AS account_version, t.id AS assignment_id, t.version AS assignment_version, u.id AS submission_id, u.version AS submission_version"

// ExportCompletedOpening keeps decrypted data only in the in-memory workbook.
func (s *Service) ExportCompletedOpening(ctx context.Context, actor Actor) ([]byte, int, error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	if err := s.CheckCompletedExport(actor); err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.completedExportQuery().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, fail(400, "mailbox_export_completed_empty")
	}
	if total > completedExportLimit {
		return nil, 0, fail(400, "mailbox_export_completed_limit")
	}
	cipher, err := s.Cipher()
	if err != nil {
		return nil, 0, fail(503, "mailbox_credentials_unavailable")
	}
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"
	versions := make([]completedExportVersion, 0, total)
	nonstandard := false
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	formatTime := func(value int64) string {
		if value == 0 {
			return ""
		}
		return time.Unix(value, 0).In(zone).Format("2006-01-02 15:04:05")
	}
	var lastID int64
	for {
		if err := s.CheckCompletedExport(actor); err != nil {
			return nil, 0, err
		}
		var rows []completedExportRow
		err := s.completedExportQuery().Select(completedVersionSelect+", a.email, a.ciphertext, a.key_version, o.display_name AS operator_name, t.status, u.remark, u.review_reason, u.created_at, u.reviewed_at").Where("a.id > ?", lastID).Order("a.id").Limit(250).Scan(&rows).Error
		if err != nil {
			return nil, 0, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if len(versions) >= completedExportLimit {
				return nil, 0, fail(400, "mailbox_export_completed_limit")
			}
			payload, err := cipher.Decrypt(row.Version.ID, mailboxCredentialKind, row.KeyVersion, row.Ciphertext)
			if err != nil {
				return nil, 0, fail(503, "mailbox_credentials_unavailable")
			}
			var secret mailboxCredentialSecret
			if json.Unmarshal([]byte(payload.Secret), &secret) != nil {
				return nil, 0, fail(503, "mailbox_credentials_unavailable")
			}
			key := mailboxOTPAbsentMarker
			if !secret.OTPAbsent {
				if _, err := secret.OTP.code(s.Now()); err != nil {
					return nil, 0, err
				}
				key = secret.OTP.Secret
			}
			status := "待审核"
			if row.Status == StatusApproved {
				status = "已通过"
			}
			cells := []string{row.Email, secret.Password, key, row.OperatorName, status, row.Remark, formatTime(row.CreatedAt), formatTime(row.ReviewedAt), row.ReviewReason}
			if !secret.OTPAbsent && (secret.OTP.Algorithm != otp.AlgorithmSHA1 || secret.OTP.Digits != otp.DigitsSix || secret.OTP.Period != 30) {
				nonstandard = true
				cells = append(cells, secret.OTP.Algorithm.String(), fmt.Sprint(int(secret.OTP.Digits)), fmt.Sprint(secret.OTP.Period))
			}
			for col, value := range cells {
				cell, _ := excelize.CoordinatesToCellName(col+1, len(versions)+2)
				if err := f.SetCellStr(sheet, cell, value); err != nil {
					return nil, 0, err
				}
			}
			versions = append(versions, row.Version)
			lastID = row.Version.ID
		}
	}
	if len(versions) == 0 {
		return nil, 0, fail(400, "mailbox_export_completed_empty")
	}
	headers := []string{"邮箱", "密码", "2FA 密钥", "操作员", "处理状态", "提交备注", "提交时间（北京时间）", "审核时间（北京时间）", "审核说明"}
	if nonstandard {
		headers = append(headers, "2FA 算法（空白为 SHA1）", "2FA 位数（空白为 6）", "2FA 周期秒（空白为 30）")
	}
	for col, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		if err := f.SetCellStr(sheet, cell, header); err != nil {
			return nil, 0, err
		}
	}
	lastCol, _ := excelize.ColumnNumberToName(len(headers))
	style, err := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}, NumFmt: 49})
	if err != nil {
		return nil, 0, err
	}
	for _, err := range []error{
		f.SetCellStyle(sheet, "A1", fmt.Sprintf("%s%d", lastCol, len(versions)+1), style),
		f.SetColWidth(sheet, "A", lastCol, 26),
		f.SetColWidth(sheet, "F", "F", 50),
		f.SetColWidth(sheet, "I", "I", 50),
		f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}),
		f.AutoFilter(sheet, fmt.Sprintf("A1:%s%d", lastCol, len(versions)+1), nil),
	} {
		if err != nil {
			return nil, 0, err
		}
	}
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, 0, err
	}
	// Recheck the selected records after generation, including credential and assignment changes.
	for start := 0; start < len(versions); start += 250 {
		end := min(start+250, len(versions))
		ids := make([]int64, 0, end-start)
		for _, version := range versions[start:end] {
			ids = append(ids, version.ID)
		}
		var current []completedExportVersion
		if err := s.completedExportQuery().Select(completedVersionSelect).Where("a.id IN ?", ids).Order("a.id").Scan(&current).Error; err != nil {
			return nil, 0, err
		}
		if len(current) != end-start {
			return nil, 0, fail(409, "mailbox_export_completed_changed")
		}
		for i, version := range current {
			if version != versions[start+i] {
				return nil, 0, fail(409, "mailbox_export_completed_changed")
			}
		}
	}
	if err := s.CheckCompletedExport(actor); err != nil {
		return nil, 0, err
	}
	return buffer.Bytes(), len(versions), nil
}
