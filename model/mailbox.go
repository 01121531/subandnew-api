package model

const MailboxAccountTypeRefund = "refund"
const MailboxAccountTypeOpening = "opening"

type MailboxAccount struct {
	ArchivedAt         int64  `json:"archived_at" gorm:"not null;default:0;index"`
	ArchivedBy         int    `json:"archived_by" gorm:"not null;default:0"`
	ID                 int64  `json:"id" gorm:"primaryKey"`
	AccountType        string `json:"account_type" gorm:"size:16;not null;default:refund;uniqueIndex:idx_mailbox_accounts_type_email,priority:1;<-:create"`
	Email              string `json:"email" gorm:"size:320;not null;uniqueIndex:idx_mailbox_accounts_type_email,priority:2"`
	Ciphertext         string `json:"-" gorm:"type:text;not null"`
	KeyVersion         string `json:"-" gorm:"size:64;not null"`
	CardCiphertext     string `json:"-" gorm:"type:text"`
	CardKeyVersion     string `json:"-" gorm:"size:64"`
	CardLast4          string `json:"card_last4" gorm:"size:4;not null;default:''"`
	Version            int64  `json:"version" gorm:"not null;default:1"`
	ActiveAssignmentID int64  `json:"-" gorm:"not null;default:0;index"`
	CreatedBy          int    `json:"created_by"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

type MailboxOperator struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	Username     string `json:"username" gorm:"size:96;not null;uniqueIndex"`
	DisplayName  string `json:"display_name" gorm:"size:128;not null"`
	PasswordHash string `json:"-" gorm:"size:255;not null"`
	Enabled      bool   `json:"enabled" gorm:"not null"`
	AuthVersion  int64  `json:"-" gorm:"not null;default:1"`
	Version      int64  `json:"version" gorm:"not null;default:1"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type MailboxSession struct {
	ID          int64  `json:"-" gorm:"primaryKey"`
	TokenHash   string `json:"-" gorm:"size:64;not null;uniqueIndex"`
	OperatorID  int64  `json:"-" gorm:"not null;index"`
	AuthVersion int64  `json:"-" gorm:"not null"`
	ExpiresAt   int64  `json:"-" gorm:"not null;index"`
	CreatedAt   int64  `json:"-"`
}

type MailboxLoginAttempt struct {
	Key         string `gorm:"size:64;primaryKey"`
	Failures    int    `gorm:"not null"`
	WindowStart int64  `gorm:"not null;index"`
}

type MailboxAssignment struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	AccountID  int64  `json:"account_id" gorm:"not null;index"`
	OperatorID int64  `json:"operator_id" gorm:"not null;index"`
	Status     string `json:"status" gorm:"size:24;not null;index"`
	Version    int64  `json:"version" gorm:"not null;default:1"`
	AssignedBy int    `json:"assigned_by"`
	RevokedAt  int64  `json:"revoked_at" gorm:"not null;default:0"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type MailboxSubmission struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	AssignmentID int64  `json:"assignment_id" gorm:"not null;index"`
	OperatorID   int64  `json:"operator_id" gorm:"not null;index"`
	Status       string `json:"status" gorm:"size:24;not null;index"`
	Version      int64  `json:"version" gorm:"not null;default:1"`
	ReviewReason string `json:"review_reason" gorm:"size:2000"`
	ReviewedBy   int    `json:"reviewed_by"`
	ReviewedAt   int64  `json:"reviewed_at"`
	CreatedAt    int64  `json:"created_at" gorm:"index"`
}

type MailboxAttachment struct {
	ID           string `json:"id" gorm:"size:64;primaryKey"`
	AssignmentID int64  `json:"assignment_id" gorm:"not null;index"`
	OperatorID   int64  `json:"-" gorm:"not null;index"`
	SubmissionID int64  `json:"submission_id" gorm:"not null;default:0;index"`
	IssueID      int64  `json:"-" gorm:"not null;default:0;index"`
	StorageKey   string `json:"-" gorm:"size:128;not null;index"`
	ContentType  string `json:"content_type" gorm:"size:64;not null"`
	Size         int64  `json:"size"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	CreatedAt    int64  `json:"created_at"`
	ExpiresAt    int64  `json:"expires_at" gorm:"not null;index"`
	DeletedAt    int64  `json:"deleted_at" gorm:"not null;default:0"`
}

type MailboxIssue struct {
	ID               int64  `gorm:"primaryKey"`
	AccountID        int64  `gorm:"not null;index"`
	AssignmentID     int64  `gorm:"not null;index"`
	OperatorID       int64  `gorm:"not null;index"`
	OpenAssignmentID *int64 `gorm:"uniqueIndex"`
	SubmittedVersion int64  `gorm:"not null;default:0"`
	Kind             string `gorm:"size:32;not null"`
	Description      string `gorm:"size:2000;not null"`
	Status           string `gorm:"size:24;not null;index"`
	Version          int64  `gorm:"not null;default:1"`
	Resolution       string `gorm:"size:16"`
	Reply            string `gorm:"size:2000"`
	ResolvedBy       int    `gorm:"not null;default:0"`
	ResolvedAt       int64  `gorm:"not null;default:0"`
	CreatedAt        int64  `gorm:"not null;index"`
}

type MailboxAudit struct {
	ID               int64  `json:"id" gorm:"primaryKey"`
	AccountType      string `json:"account_type" gorm:"size:16;not null;default:refund;index"`
	AdminID          int    `json:"admin_id" gorm:"index"`
	OperatorID       int64  `json:"operator_id" gorm:"index"`
	TargetOperatorID int64  `json:"target_operator_id" gorm:"index"`
	AccountID        int64  `json:"account_id" gorm:"index"`
	AssignmentID     int64  `json:"assignment_id"`
	Action           string `json:"action" gorm:"size:64;not null"`
	StatusCode       int    `json:"status_code"`
	ErrorCode        string `json:"error_code" gorm:"size:96"`
	IPAddress        string `json:"ip_address" gorm:"size:64"`
	CreatedAt        int64  `json:"created_at" gorm:"index"`
}
