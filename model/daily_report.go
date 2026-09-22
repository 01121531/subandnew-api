package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	DailyReportSourceManagedAccounts = "managed_accounts"
	DailyReportSourceNevermore       = "nevermore"
	DailyReportSourceRouter          = "router"
	DailyReportModeNaturalDay        = "natural_day"
	DailyReportModeRolling           = "rolling"
)

type DailyReportRule struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	InstanceID   int64  `json:"instance_id" gorm:"not null;uniqueIndex:uidx_daily_report_rule_instance_supplier"`
	SupplierCode string `json:"supplier_code" gorm:"type:varchar(128);not null;uniqueIndex:uidx_daily_report_rule_instance_supplier"`
	SupplierName string `json:"supplier_name" gorm:"type:varchar(256);not null"`
	FilterJSON   string `json:"-" gorm:"type:text;not null"`
	Enabled      bool   `json:"enabled" gorm:"not null;default:true;index"`
	CreatedBy    int    `json:"created_by" gorm:"not null;index"`
	UpdatedBy    int    `json:"updated_by" gorm:"not null"`
	Version      int64  `json:"version" gorm:"not null;default:1"`
	CreatedAt    int64  `json:"created_at" gorm:"not null;index"`
	UpdatedAt    int64  `json:"updated_at" gorm:"not null;index"`
}

func (DailyReportRule) TableName() string { return "daily_report_rules" }

func (r *DailyReportRule) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.Version == 0 {
		r.Version = 1
	}
	r.UpdatedAt = now
	return nil
}

type DailyReportSnapshot struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	InstanceID        int64  `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_daily_report_snapshot"`
	SnapshotDate      string `json:"snapshot_date" gorm:"type:varchar(10);not null;uniqueIndex:uidx_daily_report_snapshot"`
	WindowStart       int64  `json:"window_start" gorm:"not null"`
	WindowEnd         int64  `json:"window_end" gorm:"not null"`
	Mode              string `json:"mode" gorm:"type:varchar(16);not null;default:'natural_day'"`
	Source            string `json:"source" gorm:"type:varchar(32);not null;uniqueIndex:uidx_daily_report_snapshot"`
	SupplierCode      string `json:"supplier_code" gorm:"type:varchar(128);not null;default:'';uniqueIndex:uidx_daily_report_snapshot"`
	SupplierName      string `json:"supplier_name" gorm:"type:varchar(256);not null;default:''"`
	FullMetricsJSON   string `json:"-" gorm:"type:text;not null"`
	FilterMetricsJSON string `json:"-" gorm:"type:text;not null"`
	AccountCount      int64  `json:"account_count" gorm:"not null;default:0"`
	ActiveCount       int64  `json:"active_count" gorm:"not null;default:0"`
	ChannelCount      int64  `json:"channel_count" gorm:"not null;default:0"`
	UploadCount       int64  `json:"upload_count" gorm:"not null;default:0"`
	Status            string `json:"status" gorm:"type:varchar(24);not null;index"`
	ErrorCode         string `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage      string `json:"error_message" gorm:"type:varchar(512)"`
	Stale             bool   `json:"stale" gorm:"not null;default:false"`
	ObservedAt        int64  `json:"observed_at" gorm:"not null;index"`
	CreatedAt         int64  `json:"created_at" gorm:"not null"`
	UpdatedAt         int64  `json:"updated_at" gorm:"not null;index"`
}

func (DailyReportSnapshot) TableName() string { return "daily_report_snapshots" }

type DailyReportSchedule struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	OwnerID    int    `json:"owner_id" gorm:"not null;index"`
	Name       string `json:"name" gorm:"type:varchar(128);not null"`
	ConfigJSON string `json:"-" gorm:"type:text;not null"`
	Enabled    bool   `json:"enabled" gorm:"not null;default:false;index"`
	Version    int64  `json:"version" gorm:"not null;default:1"`
	NextAt     int64  `json:"next_at" gorm:"not null;default:0;index"`
	DeletedAt  int64  `json:"-" gorm:"not null;default:0;index"`
	LastRunAt  int64  `json:"last_run_at" gorm:"not null;default:0"`
	ErrorCode  string `json:"error_code" gorm:"type:varchar(128)"`
	CreatedAt  int64  `json:"created_at" gorm:"not null"`
	UpdatedAt  int64  `json:"updated_at" gorm:"not null"`
}

func (DailyReportSchedule) TableName() string { return "daily_report_schedules" }

type DailyReportRun struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	ScheduleID   int64  `json:"schedule_id" gorm:"not null;index;uniqueIndex:uidx_daily_report_run"`
	Occurrence   string `json:"occurrence" gorm:"type:varchar(80);not null;uniqueIndex:uidx_daily_report_run"`
	Status       string `json:"status" gorm:"type:varchar(24);not null;index"`
	ErrorCode    string `json:"error_code" gorm:"type:varchar(128)"`
	ReportTaskID string `json:"report_task_id" gorm:"type:varchar(64)"`
	StartedAt    int64  `json:"started_at" gorm:"not null"`
	FinishedAt   int64  `json:"finished_at" gorm:"not null;default:0"`
}

func (DailyReportRun) TableName() string { return "daily_report_runs" }
