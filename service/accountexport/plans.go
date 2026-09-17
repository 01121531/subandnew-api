package accountexport

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/billingalert"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"gorm.io/gorm"
)

type PlanInput struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Version int64  `json:"version"`
	Config  Config `json:"config"`
}
type PlanView struct {
	model.ManagedAccountExportSchedule
	Settings Config `json:"config"`
}

func SMTPReady() error {
	setting, err := billingalert.GetSMTPSetting()
	if err != nil || !setting.Enabled || strings.TrimSpace(setting.Host) == "" || setting.Port <= 0 || setting.FromAddress == "" {
		return billingalert.ErrSMTPNotConfigured
	}
	return nil
}

func LoadPlan(id int64, actorID int) (*model.ManagedAccountExportSchedule, error) {
	access, err := authz.LoadDataAccess(model.DB, actorID)
	if err != nil || !access.Can(authz.ManagedInstanceUsageView) {
		return nil, authz.ErrDataForbidden
	}
	var plan model.ManagedAccountExportSchedule
	q := model.DB.Where("id = ? AND deleted_at = 0", id)
	if access.Role < common.RoleRootUser {
		q = q.Where("owner_id = ?", actorID)
	}
	if err = q.First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func View(plan model.ManagedAccountExportSchedule) (PlanView, error) {
	view := PlanView{ManagedAccountExportSchedule: plan}
	err := json.Unmarshal([]byte(plan.Config), &view.Settings)
	return view, err
}

func ListPlans(actorID, page, pageSize int) ([]PlanView, int64, error) {
	a, err := authz.LoadDataAccess(model.DB, actorID)
	if err != nil || !a.Can(authz.ManagedInstanceUsageView) {
		return nil, 0, authz.ErrDataForbidden
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	q := model.DB.Model(&model.ManagedAccountExportSchedule{}).Where("deleted_at = 0")
	if a.Role < common.RoleRootUser {
		q = q.Where("owner_id = ?", actorID)
	}
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var plans []model.ManagedAccountExportSchedule
	if err = q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&plans).Error; err != nil {
		return nil, 0, err
	}
	views := make([]PlanView, 0, len(plans))
	for _, plan := range plans {
		view, err := View(plan)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, view)
	}
	return views, total, a.Current(model.DB)
}

func normalizeConfig(owner int, c Config) (Config, error) {
	if err := c.Schedule.Validate(); err != nil {
		return c, err
	}
	if _, err := c.Period.Window(time.Now()); err != nil {
		return c, err
	}
	if c.Period.Kind == "" {
		c.Period.Kind = "last30"
	}
	if c.Query.PresetDays == 0 {
		c.Query.PresetDays = 30
	}
	c.Query.Page, c.Query.PageSize = 1, 20
	var err error
	c.Query, err = managedaccount.NormalizeQuery(c.Query)
	if err != nil {
		return c, ErrInvalidSchedule
	}
	if c.Scope != "dynamic" && c.Scope != "fixed" {
		return c, ErrInvalidSchedule
	}
	if c.Scope == "dynamic" {
		c.Accounts = nil
	} else {
		if len(c.Accounts) == 0 || len(c.Accounts) > 10000 {
			return c, ErrAccountLimit
		}
		allowed := map[int64]bool{}
		for _, id := range c.Query.InstanceIDs {
			allowed[id] = true
		}
		seen := map[managedaccount.AccountIdentity]bool{}
		for i, item := range c.Accounts {
			item.AccountID = strings.TrimSpace(item.AccountID)
			key := managedaccount.AccountIdentity{InstanceID: item.InstanceID, AccountID: item.AccountID}
			if !allowed[item.InstanceID] || item.AccountID == "" || len(item.AccountID) > 200 || seen[key] {
				return c, ErrInvalidSchedule
			}
			seen[key] = true
			c.Accounts[i] = item
		}
	}
	c.Recipients, err = billingalert.ParseRecipientList(strings.Join(c.Recipients, "\n"))
	if err != nil || len(c.Recipients) == 0 {
		return c, ErrInvalidSchedule
	}
	if c.Locale != "en" && c.Locale != "en-US" {
		c.Locale = "zh-CN"
	}
	_, err = CheckOwner(owner, c)
	return c, err
}

func SavePlan(actorID int, id int64, input PlanInput) (*PlanView, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 128 || strings.ContainsFunc(input.Name, unicode.IsControl) {
		return nil, ErrInvalidSchedule
	}
	plan := &model.ManagedAccountExportSchedule{OwnerID: actorID, Version: 1}
	var err error
	if id > 0 {
		plan, err = LoadPlan(id, actorID)
		if err != nil {
			return nil, err
		}
		if plan.Version != input.Version {
			return nil, model.ErrExportScheduleConflict
		}
	}
	config, err := normalizeConfig(plan.OwnerID, input.Config)
	if err != nil {
		return nil, err
	}
	if input.Enabled {
		if err = SMTPReady(); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 1024*1024 {
		return nil, ErrInvalidSchedule
	}
	now := time.Now().Unix()
	next, err := config.Schedule.Next(time.Unix(now, 0))
	if err != nil {
		return nil, err
	}
	nextAt := next.Unix()
	if id > 0 && plan.Enabled && input.Enabled {
		var old Config
		if json.Unmarshal([]byte(plan.Config), &old) == nil && old.Schedule == config.Schedule && plan.NextAt > now {
			nextAt = plan.NextAt
		}
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if id == 0 {
			plan.Name, plan.Config, plan.Enabled, plan.NextAt, plan.CreatedAt, plan.UpdatedAt = input.Name, string(encoded), input.Enabled, nextAt, now, now
			return tx.Create(plan).Error
		}
		result := tx.Model(plan).Where("version = ? AND deleted_at = 0", input.Version).Updates(map[string]any{"name": input.Name, "config": string(encoded), "enabled": input.Enabled, "next_at": nextAt, "error_code": "", "version": gorm.Expr("version + 1"), "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return model.ErrExportScheduleConflict
		}
		if !input.Enabled {
			return cancelUnsent(tx, plan.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = model.DB.First(plan, plan.ID).Error; err != nil {
		return nil, err
	}
	view, err := View(*plan)
	return &view, err
}

func cancelUnsent(tx *gorm.DB, id int64) error {
	return tx.Model(&model.ManagedAccountExportDelivery{}).Where("run_id IN (?) AND status IN ?", tx.Model(&model.ManagedAccountExportRun{}).Select("id").Where("schedule_id = ?", id), []string{"pending", "retry"}).Updates(map[string]any{"status": "cancelled", "version": gorm.Expr("version + 1")}).Error
}

func ChangePlanState(actor int, id, version int64, action string) error {
	plan, err := LoadPlan(id, actor)
	if err != nil {
		return err
	}
	if plan.Version != version {
		return model.ErrExportScheduleConflict
	}
	updates := map[string]any{"version": gorm.Expr("version + 1"), "updated_at": time.Now().Unix()}
	switch action {
	case "resume":
		var c Config
		if err = json.Unmarshal([]byte(plan.Config), &c); err != nil {
			return err
		}
		if _, err = CheckOwner(plan.OwnerID, c); err != nil {
			return err
		}
		if err = SMTPReady(); err != nil {
			return err
		}
		next, err := c.Schedule.Next(time.Now())
		if err != nil {
			return err
		}
		updates["enabled"], updates["next_at"], updates["error_code"] = true, next.Unix(), ""
	case "pause":
		updates["enabled"] = false
	case "delete":
		updates["enabled"], updates["deleted_at"] = false, time.Now().Unix()
	default:
		return ErrInvalidSchedule
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(plan).Where("version = ? AND deleted_at = 0", version).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return model.ErrExportScheduleConflict
		}
		if action != "resume" {
			return cancelUnsent(tx, id)
		}
		return nil
	})
}

func SafeCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, authz.ErrDataForbidden), errors.Is(err, authz.ErrAuthorizationChanged):
		return "permission_revoked"
	case errors.Is(err, billingalert.ErrSMTPNotConfigured):
		return "smtp_not_configured"
	case errors.Is(err, ErrNoAccounts):
		return "no_data"
	case errors.Is(err, ErrAccountLimit):
		return "account_limit"
	case errors.Is(err, ErrSnapshotUnavailable):
		return "snapshot_unavailable"
	case errors.Is(err, ErrSnapshotChanged):
		return "snapshot_changed"
	case errors.Is(err, model.ErrExportScheduleConflict):
		return "version_conflict"
	case errors.Is(err, ErrInvalidSchedule):
		return "invalid_schedule"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "not_found"
	default:
		return "export_schedule_failed"
	}
}
