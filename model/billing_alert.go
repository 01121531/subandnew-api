package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	BillingCurrencyUSD = "USD"
	BillingCurrencyCNY = "CNY"

	BillingAlertEventThreshold = "threshold"
	BillingAlertEventFailure   = "monitor_failure"
	BillingAlertEventRecovery  = "monitor_recovery"
)

type BillingFilterTemplate struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	Name           string `json:"name" gorm:"type:varchar(128);not null;uniqueIndex"`
	Description    string `json:"description" gorm:"type:varchar(512)"`
	CurrentVersion int    `json:"current_version" gorm:"not null;default:1"`
	Enabled        bool   `json:"enabled" gorm:"not null;default:true;index"`
	CreatedBy      int    `json:"created_by" gorm:"not null;index"`
	UpdatedBy      int    `json:"updated_by" gorm:"not null"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (BillingFilterTemplate) TableName() string { return "billing_filter_templates" }

type BillingFilterTemplateVersion struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	TemplateID int64  `json:"template_id" gorm:"not null;uniqueIndex:uidx_billing_template_version,priority:1;index"`
	Version    int    `json:"version" gorm:"not null;uniqueIndex:uidx_billing_template_version,priority:2"`
	Filters    string `json:"filters" gorm:"type:text;not null"`
	CreatedBy  int    `json:"created_by" gorm:"not null;index"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (BillingFilterTemplateVersion) TableName() string {
	return "billing_filter_template_versions"
}

type BillingAlertRule struct {
	ID                 int64  `json:"id" gorm:"primaryKey"`
	Name               string `json:"name" gorm:"type:varchar(128);not null;uniqueIndex"`
	Description        string `json:"description" gorm:"type:varchar(512)"`
	TemplateID         int64  `json:"template_id" gorm:"not null;index"`
	Enabled            bool   `json:"enabled" gorm:"not null;default:true;index"`
	Timezone           string `json:"timezone" gorm:"type:varchar(64);not null;default:'Asia/Shanghai'"`
	CycleType          string `json:"cycle_type" gorm:"type:varchar(32);not null;index"`
	CycleConfig        string `json:"cycle_config" gorm:"type:text;not null"`
	DiscountRate       string `json:"discount_rate" gorm:"type:varchar(64);not null"`
	ExchangeMode       string `json:"exchange_mode" gorm:"type:varchar(32);not null;index"`
	ManualExchangeRate string `json:"manual_exchange_rate" gorm:"type:varchar(64)"`
	ExchangeOverride   bool   `json:"exchange_override" gorm:"not null;default:false"`
	ScheduleType       string `json:"schedule_type" gorm:"type:varchar(32);not null;index"`
	ScheduleConfig     string `json:"schedule_config" gorm:"type:text;not null"`
	Recipients         string `json:"recipients" gorm:"type:text;not null"`
	FailureThreshold   int    `json:"failure_threshold" gorm:"not null;default:3"`
	CreatedBy          int    `json:"created_by" gorm:"not null;index"`
	UpdatedBy          int    `json:"updated_by" gorm:"not null"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (BillingAlertRule) TableName() string { return "billing_alert_rules" }

type BillingAlertRuleInstance struct {
	ID                  int64  `json:"id" gorm:"primaryKey"`
	RuleID              int64  `json:"rule_id" gorm:"not null;uniqueIndex:uidx_billing_rule_instance,priority:1;index"`
	InstanceID          int64  `json:"instance_id" gorm:"not null;uniqueIndex:uidx_billing_rule_instance,priority:2;index"`
	Enabled             bool   `json:"enabled" gorm:"not null;default:true;index"`
	ConsecutiveFailures int    `json:"consecutive_failures" gorm:"not null;default:0"`
	FailureNotified     bool   `json:"failure_notified" gorm:"not null;default:false"`
	FilterStatus        string `json:"filter_status" gorm:"type:varchar(256)"`
	LastErrorCode       string `json:"last_error_code" gorm:"type:varchar(128)"`
	LastEvaluatedAt     int64  `json:"last_evaluated_at" gorm:"bigint;not null;default:0;index"`
	LastSucceededAt     int64  `json:"last_succeeded_at" gorm:"bigint;not null;default:0"`
	NextRunAt           int64  `json:"next_run_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt           int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt           int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (BillingAlertRuleInstance) TableName() string { return "billing_alert_rule_instances" }

type BillingAlertThreshold struct {
	ID                    int64  `json:"id" gorm:"primaryKey"`
	RuleID                int64  `json:"rule_id" gorm:"not null;index"`
	Name                  string `json:"name" gorm:"type:varchar(128);not null"`
	SortOrder             int    `json:"sort_order" gorm:"not null;default:0"`
	Severity              string `json:"severity" gorm:"type:varchar(16);not null"`
	Currency              string `json:"currency" gorm:"type:varchar(3);not null;index"`
	Amount                string `json:"amount" gorm:"type:varchar(64);not null"`
	ReminderMode          string `json:"reminder_mode" gorm:"type:varchar(32);not null"`
	RepeatIntervalSeconds int64  `json:"repeat_interval_seconds" gorm:"bigint;not null;default:0"`
	RepeatIncrement       string `json:"repeat_increment" gorm:"type:varchar(64)"`
	CreatedAt             int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt             int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (BillingAlertThreshold) TableName() string { return "billing_alert_thresholds" }

type BillingCycleSnapshot struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	RuleID          int64  `json:"rule_id" gorm:"not null;uniqueIndex:uidx_billing_cycle,priority:1;index"`
	InstanceID      int64  `json:"instance_id" gorm:"not null;uniqueIndex:uidx_billing_cycle,priority:2;index"`
	CycleKey        string `json:"cycle_key" gorm:"type:varchar(128);not null;uniqueIndex:uidx_billing_cycle,priority:3"`
	CycleStart      int64  `json:"cycle_start" gorm:"bigint;not null;index"`
	CycleEnd        int64  `json:"cycle_end" gorm:"bigint;not null;index"`
	Timezone        string `json:"timezone" gorm:"type:varchar(64);not null"`
	TemplateVersion int    `json:"template_version" gorm:"not null"`
	Filters         string `json:"filters" gorm:"type:text;not null"`
	DiscountRate    string `json:"discount_rate" gorm:"type:varchar(64);not null"`
	ExchangeMode    string `json:"exchange_mode" gorm:"type:varchar(32);not null"`
	ExchangeRate    string `json:"exchange_rate" gorm:"type:varchar(64)"`
	ExchangeRateID  int64  `json:"exchange_rate_id" gorm:"not null;default:0"`
	ThresholdState  string `json:"threshold_state" gorm:"type:text;not null"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt       int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (BillingCycleSnapshot) TableName() string { return "billing_cycle_snapshots" }

type BillingEvaluationSnapshot struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	CycleID        int64  `json:"cycle_id" gorm:"not null;index"`
	RuleID         int64  `json:"rule_id" gorm:"not null;index"`
	InstanceID     int64  `json:"instance_id" gorm:"not null;index"`
	USDTotal       string `json:"usd_total" gorm:"type:varchar(64);not null"`
	CNYTotal       string `json:"cny_total" gorm:"type:varchar(64);not null"`
	DiscountRate   string `json:"discount_rate" gorm:"type:varchar(64);not null"`
	ExchangeRate   string `json:"exchange_rate" gorm:"type:varchar(64);not null"`
	ExchangeRateID int64  `json:"exchange_rate_id" gorm:"not null;default:0"`
	RecordCount    int64  `json:"record_count" gorm:"bigint;not null;default:0"`
	DataTimestamp  int64  `json:"data_timestamp" gorm:"bigint;not null;index"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (BillingEvaluationSnapshot) TableName() string { return "billing_evaluation_snapshots" }

type BillingAlertEvent struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	EventKey             string `json:"event_key" gorm:"type:varchar(192);not null;uniqueIndex"`
	EventType            string `json:"event_type" gorm:"type:varchar(32);not null;index"`
	RuleID               int64  `json:"rule_id" gorm:"not null;index"`
	InstanceID           int64  `json:"instance_id" gorm:"not null;index"`
	CycleID              int64  `json:"cycle_id" gorm:"not null;default:0;index"`
	ThresholdID          int64  `json:"threshold_id" gorm:"not null;default:0;index"`
	EvaluationID         int64  `json:"evaluation_id" gorm:"not null;default:0;index"`
	RuleName             string `json:"rule_name_snapshot" gorm:"type:varchar(128)"`
	InstanceName         string `json:"instance_name_snapshot" gorm:"type:varchar(128)"`
	InstanceKind         string `json:"instance_kind_snapshot" gorm:"type:varchar(32)"`
	ThresholdName        string `json:"threshold_name_snapshot" gorm:"type:varchar(128)"`
	CycleStart           int64  `json:"cycle_start" gorm:"bigint;not null;default:0"`
	CycleEnd             int64  `json:"cycle_end" gorm:"bigint;not null;default:0"`
	Timezone             string `json:"timezone" gorm:"type:varchar(64)"`
	Filters              string `json:"filters" gorm:"type:text"`
	ExchangeMode         string `json:"exchange_mode" gorm:"type:varchar(32)"`
	ExchangeSource       string `json:"exchange_source" gorm:"type:varchar(32)"`
	ExchangeObservedDate string `json:"exchange_observed_date" gorm:"type:varchar(10)"`
	Currency             string `json:"currency" gorm:"type:varchar(3)"`
	Threshold            string `json:"threshold" gorm:"type:varchar(64)"`
	USDTotal             string `json:"usd_total" gorm:"type:varchar(64)"`
	CNYTotal             string `json:"cny_total" gorm:"type:varchar(64)"`
	DiscountRate         string `json:"discount_rate" gorm:"type:varchar(64)"`
	ExchangeRate         string `json:"exchange_rate" gorm:"type:varchar(64)"`
	Recipients           string `json:"recipients" gorm:"type:text;not null"`
	ErrorCode            string `json:"error_code" gorm:"type:varchar(128)"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (BillingAlertEvent) TableName() string { return "billing_alert_events" }

type BillingEmailDelivery struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	EventID     int64  `json:"event_id" gorm:"not null;index"`
	Recipient   string `json:"recipient" gorm:"type:varchar(320);not null;index"`
	Status      string `json:"status" gorm:"type:varchar(32);not null;index"`
	Attempts    int    `json:"attempts" gorm:"not null;default:0"`
	LastError   string `json:"last_error" gorm:"type:text"`
	NextRetryAt int64  `json:"next_retry_at" gorm:"bigint;not null;default:0;index"`
	SentAt      int64  `json:"sent_at" gorm:"bigint;not null;default:0"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (BillingEmailDelivery) TableName() string { return "billing_email_deliveries" }

type ExchangeRate struct {
	ID            int64  `json:"id" gorm:"primaryKey"`
	BaseCurrency  string `json:"base_currency" gorm:"type:varchar(3);not null;uniqueIndex:uidx_exchange_rate,priority:1"`
	QuoteCurrency string `json:"quote_currency" gorm:"type:varchar(3);not null;uniqueIndex:uidx_exchange_rate,priority:2"`
	ObservedDate  string `json:"observed_date" gorm:"type:varchar(10);not null;uniqueIndex:uidx_exchange_rate,priority:3;index"`
	Source        string `json:"source" gorm:"type:varchar(32);not null;uniqueIndex:uidx_exchange_rate,priority:4;index"`
	Rate          string `json:"rate" gorm:"type:varchar(64);not null"`
	Fallback      bool   `json:"fallback" gorm:"not null;default:false"`
	FetchedAt     int64  `json:"fetched_at" gorm:"bigint;not null;index"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (ExchangeRate) TableName() string { return "exchange_rates" }

type ExchangeRateSetting struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	Automatic       bool   `json:"automatic" gorm:"not null;default:true"`
	DefaultMode     string `json:"default_mode" gorm:"type:varchar(32);not null;default:'latest'"`
	ManualRate      string `json:"manual_rate" gorm:"type:varchar(64)"`
	PrimarySource   string `json:"primary_source" gorm:"type:varchar(32);not null;default:'ecb'"`
	FallbackSource  string `json:"fallback_source" gorm:"type:varchar(32);not null;default:'frankfurter'"`
	UpdateTimes     string `json:"update_times" gorm:"type:text;not null"`
	Timezone        string `json:"timezone" gorm:"type:varchar(64);not null;default:'Asia/Shanghai'"`
	LatestRateID    int64  `json:"latest_rate_id" gorm:"not null;default:0"`
	LastAttemptAt   int64  `json:"last_attempt_at" gorm:"bigint;not null;default:0"`
	LastSucceededAt int64  `json:"last_succeeded_at" gorm:"bigint;not null;default:0"`
	LastErrorCode   string `json:"last_error_code" gorm:"type:varchar(128)"`
	UpdatedBy       int    `json:"updated_by" gorm:"not null;default:0"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt       int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (ExchangeRateSetting) TableName() string { return "exchange_rate_settings" }

type SMTPSetting struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	Host           string `json:"host" gorm:"type:varchar(255);not null"`
	Port           int    `json:"port" gorm:"not null;default:587"`
	Security       string `json:"security" gorm:"type:varchar(16);not null;default:'starttls'"`
	Username       string `json:"username" gorm:"type:varchar(320)"`
	PasswordCipher string `json:"-" gorm:"type:text"`
	KeyVersion     string `json:"-" gorm:"type:varchar(32)"`
	FromName       string `json:"from_name" gorm:"type:varchar(128)"`
	FromAddress    string `json:"from_address" gorm:"type:varchar(320);not null"`
	ReplyTo        string `json:"reply_to" gorm:"type:varchar(320)"`
	Enabled        bool   `json:"enabled" gorm:"not null;default:false"`
	UpdatedBy      int    `json:"updated_by" gorm:"not null;default:0"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (SMTPSetting) TableName() string { return "smtp_settings" }

type BillingAudit struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	ActorID    int    `json:"actor_id" gorm:"not null;index"`
	Action     string `json:"action" gorm:"type:varchar(64);not null;index"`
	Resource   string `json:"resource" gorm:"type:varchar(64);not null;index"`
	ResourceID int64  `json:"resource_id" gorm:"not null;default:0;index"`
	Outcome    string `json:"outcome" gorm:"type:varchar(32);not null;index"`
	Details    string `json:"details" gorm:"type:text"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (BillingAudit) TableName() string { return "billing_audits" }

type BillingAlertExport struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	TaskID      string `json:"task_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	ActorID     int    `json:"actor_id" gorm:"not null;index"`
	Query       string `json:"query" gorm:"type:text;not null"`
	Status      string `json:"status" gorm:"type:varchar(32);not null;index"`
	FileName    string `json:"file_name" gorm:"type:varchar(255)"`
	FilePath    string `json:"-" gorm:"type:text"`
	FileSize    int64  `json:"file_size" gorm:"bigint;not null;default:0"`
	RecordCount int64  `json:"record_count" gorm:"bigint;not null;default:0"`
	ErrorCode   string `json:"error_code" gorm:"type:varchar(128)"`
	StartedAt   int64  `json:"started_at" gorm:"bigint;not null;default:0"`
	FinishedAt  int64  `json:"finished_at" gorm:"bigint;not null;default:0"`
	ExpiresAt   int64  `json:"expires_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (BillingAlertExport) TableName() string { return "billing_alert_exports" }

func billingModelBeforeCreate(createdAt *int64, updatedAt *int64) {
	now := common.GetTimestamp()
	if *createdAt == 0 {
		*createdAt = now
	}
	if updatedAt != nil {
		*updatedAt = now
	}
}

func (m *BillingFilterTemplate) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
func (m *BillingFilterTemplateVersion) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, nil)
	return nil
}
func (m *BillingAlertRule) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
func (m *BillingAlertRuleInstance) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
func (m *BillingAlertThreshold) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
func (m *BillingCycleSnapshot) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
func (m *BillingEvaluationSnapshot) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, nil)
	return nil
}
func (m *BillingAlertEvent) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, nil)
	return nil
}
func (m *BillingEmailDelivery) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
func (m *ExchangeRate) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, nil)
	return nil
}
func (m *ExchangeRateSetting) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
func (m *SMTPSetting) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
func (m *BillingAudit) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, nil)
	return nil
}
func (m *BillingAlertExport) BeforeCreate(_ *gorm.DB) error {
	billingModelBeforeCreate(&m.CreatedAt, &m.UpdatedAt)
	return nil
}
