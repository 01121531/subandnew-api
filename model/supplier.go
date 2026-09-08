package model

import "gorm.io/gorm"

type Supplier struct {
	ID             int64          `json:"id" gorm:"primaryKey"`
	Name           string         `json:"name" gorm:"type:varchar(96);not null"`
	Username       string         `json:"username" gorm:"type:varchar(96);not null;uniqueIndex"`
	PasswordHash   string         `json:"-" gorm:"type:varchar(100);not null"`
	Enabled        bool           `json:"enabled" gorm:"not null"`
	ViewAccounts   bool           `json:"view_accounts" gorm:"not null"`
	ViewUsage      bool           `json:"view_usage" gorm:"not null"`
	ManageProxies  bool           `json:"manage_proxies" gorm:"not null"`
	UploadAccounts bool           `json:"upload_accounts" gorm:"not null"`
	AuthVersion    int64          `json:"-" gorm:"not null"`
	CreatedAt      int64          `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      int64          `json:"updated_at" gorm:"autoUpdateTime"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`
}

type SupplierBinding struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	SupplierID     int64  `json:"supplier_id" gorm:"not null;uniqueIndex:uidx_supplier_instance"`
	InstanceID     int64  `json:"instance_id" gorm:"not null;uniqueIndex:uidx_supplier_instance;uniqueIndex:uidx_supplier_remote"`
	RemoteUserID   string `json:"-" gorm:"type:varchar(128);not null;uniqueIndex:uidx_supplier_remote"`
	RemoteUsername string `json:"remote_username" gorm:"type:varchar(128)"`
	Ciphertext     string `json:"-" gorm:"type:text;not null"`
	KeyVersion     string `json:"-" gorm:"type:varchar(32);not null"`
	Revision       string `json:"-" gorm:"type:varchar(64);not null"`
	CacheVersion   int64  `json:"-" gorm:"not null"`
	Enabled        bool   `json:"enabled" gorm:"not null"`
	CreatedAt      int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      int64  `json:"updated_at" gorm:"autoUpdateTime"`
	InstanceName   string `json:"instance_name" gorm:"-"`
}

type SupplierSession struct {
	ID          int64  `json:"-" gorm:"primaryKey"`
	SupplierID  int64  `json:"-" gorm:"not null;index"`
	TokenHash   string `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	AuthVersion int64  `json:"-" gorm:"not null"`
	ExpiresAt   int64  `json:"expires_at" gorm:"not null;index"`
	LastUsedAt  int64  `json:"-" gorm:"not null"`
	CreatedAt   int64  `json:"-" gorm:"autoCreateTime"`
}

type SupplierOAuthFlow struct {
	ID              int64  `gorm:"primaryKey"`
	TokenHash       string `gorm:"type:char(64);not null;uniqueIndex"`
	SupplierID      int64  `gorm:"not null;index"`
	SessionID       int64  `gorm:"not null;index"`
	BindingID       int64  `gorm:"not null;index"`
	BindingRevision string `gorm:"type:varchar(64);not null"`
	Ciphertext      string `gorm:"type:text;not null"`
	KeyVersion      string `gorm:"type:varchar(32);not null"`
	ExpiresAt       int64  `gorm:"not null;index"`
	ConsumedAt      int64  `gorm:"not null"`
	CreatedAt       int64  `gorm:"autoCreateTime"`
}

func (SupplierOAuthFlow) TableName() string { return "supplier_oauth_flows" }

type SupplierAudit struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	SupplierID int64  `json:"supplier_id" gorm:"not null;index:idx_supplier_audits_time,priority:1"`
	BindingID  int64  `json:"binding_id" gorm:"not null;index"`
	AdminID    int    `json:"admin_id" gorm:"not null"`
	Action     string `json:"action" gorm:"type:varchar(128);not null"`
	IPAddress  string `json:"ip_address" gorm:"type:varchar(64);not null"`
	StatusCode int    `json:"status_code" gorm:"not null"`
	DurationMS int64  `json:"duration_ms" gorm:"not null"`
	ErrorCode  string `json:"error_code" gorm:"type:varchar(64)"`
	CreatedAt  int64  `json:"created_at" gorm:"autoCreateTime;index:idx_supplier_audits_time,priority:2"`
}
