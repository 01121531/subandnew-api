package model

// CVV never participates in account/list DTOs or export projections.
type MailboxCVV struct {
	AccountID  int64  `gorm:"primaryKey;autoIncrement:false" json:"-"`
	Ciphertext string `gorm:"type:text;not null" json:"-"`
	KeyVersion string `gorm:"size:64;not null" json:"-"`
	Version    int64  `gorm:"not null" json:"-"`
	UpdatedAt  int64  `json:"-"`
}

type MailboxCardIndex struct {
	AccountID int64  `gorm:"primaryKey;autoIncrement:false"`
	Kind      string `gorm:"primaryKey;size:8"`
	Length    int    `gorm:"primaryKey;autoIncrement:false"`
	Digest    string `gorm:"size:64;not null;index"`
}

type MailboxCardIndexState struct {
	AccountID   int64  `gorm:"primaryKey;autoIncrement:false"`
	Fingerprint string `gorm:"size:64;not null"`
}
