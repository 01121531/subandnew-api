package accountexport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/billingalert"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

var lifecycle struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func Start() {
	if !common.IsMasterNode {
		return
	}
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if lifecycle.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lifecycle.cancel, lifecycle.done = cancel, done
	go func() {
		defer close(done)
		worker := "account-export-" + common.GetRandomString(16)
		if err := skipMissed(time.Now()); err != nil {
			common.SysError("account export scheduler startup failed")
			return
		}
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			if err := tick(ctx, worker); err != nil {
				common.SysError("account export scheduler pass failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func Stop(ctx context.Context) error {
	lifecycle.Lock()
	cancel, done := lifecycle.cancel, lifecycle.done
	lifecycle.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func skipMissed(now time.Time) error {
	var plans []model.ManagedAccountExportSchedule
	if err := model.DB.Where("enabled = ? AND deleted_at = 0 AND next_at <= ?", true, now.Unix()).Find(&plans).Error; err != nil {
		return err
	}
	for _, plan := range plans {
		var c Config
		if err := json.Unmarshal([]byte(plan.Config), &c); err != nil {
			return err
		}
		next, err := c.Schedule.Next(now)
		if err != nil {
			return err
		}
		if err = model.DB.Model(&model.ManagedAccountExportSchedule{}).Where("id = ? AND version = ?", plan.ID, plan.Version).Updates(map[string]any{"next_at": next.Unix(), "version": gorm.Expr("version + 1"), "updated_at": now.Unix()}).Error; err != nil {
			return err
		}
	}
	return nil
}

func ExecuteNow(ctx context.Context, actor int, id, version int64) (*model.ManagedAccountExportRun, error) {
	plan, err := LoadPlan(id, actor)
	if err != nil {
		return nil, err
	}
	if plan.Version != version {
		return nil, model.ErrExportScheduleConflict
	}
	if !plan.Enabled {
		return nil, model.ErrExportScheduleConflict
	}
	if err = SMTPReady(); err != nil {
		return nil, err
	}
	return execute(ctx, *plan, true, "manual-"+common.GetRandomString(16))
}

func execute(ctx context.Context, plan model.ManagedAccountExportSchedule, manual bool, worker string) (*model.ManagedAccountExportRun, error) {
	var config Config
	if err := json.Unmarshal([]byte(plan.Config), &config); err != nil {
		return nil, err
	}
	now := time.Now()
	scheduledAt := plan.NextAt
	occurrence := fmt.Sprintf("scheduled:%d", scheduledAt)
	if manual {
		scheduledAt = now.Unix()
		occurrence = "manual:" + common.GetRandomString(24)
	}
	next, err := config.Schedule.Next(now)
	if err != nil {
		return nil, err
	}
	var prepared *PreparedRun
	var access *authz.DataAccess
	var prepareErr error
	if plan.ActiveRunID == 0 {
		access, prepareErr = CheckOwner(plan.OwnerID, config)
		if prepareErr == nil {
			prepareErr = SMTPReady()
		}
		if prepareErr == nil {
			prepared, prepareErr = PrepareRun(ctx, plan.OwnerID, config, time.Unix(scheduledAt, 0))
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var run *model.ManagedAccountExportRun
	err = model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		run, err = model.ClaimAccountExportOccurrence(tx, plan, occurrence, scheduledAt, next.Unix(), now.Unix(), manual, worker)
		if err != nil {
			return err
		}
		if run.Status == "skipped" {
			return nil
		}
		if prepareErr == nil && access != nil {
			prepareErr = access.Current(tx)
		}
		if prepareErr != nil {
			code := SafeCode(prepareErr)
			status := "failed"
			if code == "no_data" {
				status = "no_data"
			}
			updates := map[string]any{"active_run_id": 0}
			if code == "permission_revoked" || code == "smtp_not_configured" {
				updates["enabled"], updates["error_code"] = false, code
			}
			if err = tx.Model(&model.ManagedAccountExportSchedule{}).Where("id = ?", plan.ID).Updates(updates).Error; err != nil {
				return err
			}
			run.Status, run.ErrorCode, run.FinishedAt = status, code, now.Unix()
			run.LeaseOwner, run.LeaseUntil = "", 0
			return tx.Save(run).Error
		}
		prepared.Export.Record.ScheduleID = plan.ID
		zone, _ := time.LoadLocation("Asia/Shanghai")
		notes := []string{fmt.Sprintf("Scheduled report / 计划 ID: %d", plan.ID), "Account scope: latest successful local snapshots. Usage: requested report window; unsupported metrics are not provided."}
		if access.HasField("time") {
			notes = append(notes, "Planned time / 计划时间: "+time.Unix(scheduledAt, 0).In(zone).Format("2006-01-02 15:04:05"))
			for _, source := range prepared.Sources {
				notes = append(notes, fmt.Sprintf("Instance %d / 快照: %s; stale / 旧数据: %t", source.InstanceID, time.Unix(source.ObservedAt, 0).In(zone).Format("2006-01-02 15:04:05"), source.Stale))
			}
		}
		if prepared.MissingCount > 0 {
			notes = append(notes, fmt.Sprintf("Warning / 警告: %d selected accounts missing from latest snapshot", prepared.MissingCount))
		}
		prepared.Export.Payload.Notes = notes
		prepared.Export.Payload.SnapshotTimes = make(map[int64]int64)
		for _, source := range prepared.Sources {
			prepared.Export.Payload.SnapshotTimes[source.InstanceID] = source.ObservedAt
		}
		task, err := model.CreateManagedUsageExportWithItemsTx(tx, prepared.Export.Record, prepared.Export.Payload, prepared.Export.State, prepared.Export.Items)
		if err != nil {
			return err
		}
		run.TaskID, run.Status, run.MissingCount = task.TaskID, "exporting", prepared.MissingCount
		run.LeaseOwner, run.LeaseUntil = "", 0
		if err = tx.Save(run).Error; err != nil {
			return err
		}
		for _, recipient := range config.Recipients {
			delivery := model.ManagedAccountExportDelivery{RunID: run.ID, Recipient: recipient, Status: "pending", Version: 1, CreatedAt: now.Unix()}
			if err = tx.Create(&delivery).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return run, err
}

func tick(ctx context.Context, worker string) error {
	now := time.Now().Unix()
	if err := model.RecoverAccountExportDeliveries(model.DB, now); err != nil {
		return err
	}
	var plans []model.ManagedAccountExportSchedule
	if err := model.DB.Where("enabled = ? AND deleted_at = 0 AND next_at <= ?", true, now).Order("next_at ASC").Limit(50).Find(&plans).Error; err != nil {
		return err
	}
	for _, plan := range plans {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := execute(ctx, plan, false, worker); err != nil && !errors.Is(err, model.ErrExportScheduleConflict) {
			common.SysError("account export schedule execution failed")
		}
	}
	var last int64
	for {
		var runs []model.ManagedAccountExportRun
		if err := model.DB.Where("id > ? AND status IN ?", last, []string{"exporting", "delivering"}).Order("id ASC").Limit(50).Find(&runs).Error; err != nil {
			return err
		}
		if len(runs) == 0 {
			break
		}
		for _, run := range runs {
			last = run.ID
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := processRun(ctx, worker, run); err != nil {
				common.SysError("account export delivery processing failed")
			}
		}
	}
	return nil
}

func processRun(ctx context.Context, worker string, run model.ManagedAccountExportRun) error {
	var plan model.ManagedAccountExportSchedule
	if err := model.DB.First(&plan, run.ScheduleID).Error; err != nil {
		return err
	}
	record, err := model.GetManagedUsageExport(run.TaskID)
	if err != nil {
		return finishRun(run, "failed", "export_unavailable")
	}
	if err := service.CheckScheduledExportOwner(record, true); err != nil {
		_ = suspend(plan.ID, "permission_revoked")
		return finishRun(run, "failed", "permission_revoked")
	}
	if record.Status == model.ManagedUsageExportStatusPending || record.Status == model.ManagedUsageExportStatusRunning {
		return nil
	}
	if record.Status != model.ManagedUsageExportStatusSucceeded {
		return finishRun(run, "failed", "export_failed")
	}
	if !plan.Enabled || plan.DeletedAt != 0 {
		if err = cancelUnsent(model.DB, plan.ID); err != nil {
			return err
		}
	}
	if record.ExpiresAt <= time.Now().Unix() {
		return finishRun(run, "failed", "file_expired")
	}
	if record.FileSize > billingalert.MaxSMTPAttachmentBytes {
		return finishRun(run, "attachment_too_large", "attachment_too_large")
	}
	var deliveries []model.ManagedAccountExportDelivery
	if err = model.DB.Where("run_id = ? AND status IN ? AND next_attempt_at <= ?", run.ID, []string{"pending", "retry"}, time.Now().Unix()).Order("id ASC").Find(&deliveries).Error; err != nil {
		return err
	}
	for _, delivery := range deliveries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = deliver(ctx, worker, plan, run, record, delivery); err != nil {
			return err
		}
	}
	var pending int64
	if err = model.DB.Model(&model.ManagedAccountExportDelivery{}).Where("run_id = ? AND status IN ?", run.ID, []string{"pending", "retry", "sending"}).Count(&pending).Error; err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	var notSent int64
	if err = model.DB.Model(&model.ManagedAccountExportDelivery{}).Where("run_id = ? AND status <> ?", run.ID, "sent").Count(&notSent).Error; err != nil {
		return err
	}
	if notSent > 0 {
		return finishRun(run, "delivery_failed", "delivery_attention_required")
	}
	return finishRun(run, "succeeded", "")
}

func suspend(id int64, code string) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ManagedAccountExportSchedule{}).Where("id = ?", id).Updates(map[string]any{"enabled": false, "error_code": code, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		return cancelUnsent(tx, id)
	})
}

func finishRun(run model.ManagedAccountExportRun, status, code string) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ManagedAccountExportRun{}).Where("id = ? AND status IN ?", run.ID, []string{"exporting", "delivering"}).Updates(map[string]any{"status": status, "error_code": code, "finished_at": time.Now().Unix()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ManagedAccountExportDelivery{}).Where("run_id = ? AND status IN ?", run.ID, []string{"pending", "retry"}).Updates(map[string]any{"status": "cancelled", "error_code": code, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		return tx.Model(&model.ManagedAccountExportSchedule{}).Where("id = ? AND active_run_id = ?", run.ScheduleID, run.ID).Update("active_run_id", 0).Error
	})
}

func deliver(ctx context.Context, worker string, plan model.ManagedAccountExportSchedule, run model.ManagedAccountExportRun, record *model.ManagedUsageExport, delivery model.ManagedAccountExportDelivery) error {
	if err := SMTPReady(); err != nil {
		return suspend(plan.ID, "smtp_not_configured")
	}
	file, err := managedinstance.OpenManagedExportArtifact(record.TaskID, record.FileFormat)
	if err != nil {
		return finishRun(run, "failed", "file_unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(file, billingalert.MaxSMTPAttachmentBytes+1))
	_ = file.Close()
	if err != nil {
		return err
	}
	if len(data) > billingalert.MaxSMTPAttachmentBytes {
		return finishRun(run, "attachment_too_large", "attachment_too_large")
	}
	if err = service.CheckScheduledExportOwner(record, true); err != nil {
		_ = suspend(plan.ID, "permission_revoked")
		return nil
	}
	claimed := false
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		guard := tx.Model(&model.ManagedAccountExportSchedule{}).Where("id = ? AND enabled = ? AND deleted_at = 0 AND active_run_id = ?", plan.ID, true, run.ID).Update("version", gorm.Expr("version + 1"))
		if guard.Error != nil {
			return guard.Error
		}
		if guard.RowsAffected != 1 {
			return nil
		}
		result := tx.Model(&model.ManagedAccountExportDelivery{}).Where("id = ? AND version = ? AND status IN ?", delivery.ID, delivery.Version, []string{"pending", "retry"}).Updates(map[string]any{"status": "sending", "lease_owner": worker, "lease_until": time.Now().Unix() + 120, "attempts": gorm.Expr("attempts + 1"), "version": gorm.Expr("version + 1")})
		claimed = result.RowsAffected == 1
		return result.Error
	})
	if err != nil || !claimed {
		return err
	}
	// Last checks occur after the durable sending marker. Once SMTP starts,
	// interruption is uncertain and must never become an automatic replay.
	var current model.ManagedAccountExportSchedule
	checkErr := model.DB.First(&current, plan.ID).Error
	if checkErr == nil && (!current.Enabled || current.DeletedAt != 0) {
		checkErr = model.ErrExportScheduleConflict
	}
	if checkErr == nil {
		checkErr = service.CheckScheduledExportOwner(record, true)
	}
	status, code := "cancelled", SafeCode(checkErr)
	nextAttempt := int64(0)
	if checkErr == nil {
		mailCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err = billingalert.SendSMTPMessage(mailCtx, billingalert.SMTPMessage{Recipients: []string{delivery.Recipient}, Subject: plan.Name + " / 账号报表", TextBody: "账号导出报表见附件。附件发送后无法撤回，请妥善保存。", Attachments: []billingalert.SMTPAttachment{{Name: record.FileName, Data: data}}})
		cancel()
		status = billingalert.SMTPFailureClass(err)
		code = ""
		if err != nil {
			code = "smtp_" + status
		}
		if status == "temporary" {
			if delay, ok := DeliveryRetryDelay(delivery.Attempts); ok {
				status = "retry"
				nextAttempt = time.Now().Add(delay).Unix()
			} else {
				status = "failed"
			}
		}
		if status == "permanent" {
			status = "failed"
		}
	}
	updates := map[string]any{"status": status, "error_code": code, "next_attempt_at": nextAttempt, "lease_owner": "", "lease_until": 0, "version": gorm.Expr("version + 1")}
	if status == "sent" {
		updates["sent_at"] = time.Now().Unix()
	}
	return model.DB.Model(&model.ManagedAccountExportDelivery{}).Where("id = ? AND status = ? AND lease_owner = ?", delivery.ID, "sending", worker).Updates(updates).Error
}

func RetryDelivery(actor int, planID, deliveryID, version int64, confirmed bool) error {
	plan, err := LoadPlan(planID, actor)
	if err != nil {
		return err
	}
	if !plan.Enabled {
		return model.ErrExportScheduleConflict
	}
	if err = SMTPReady(); err != nil {
		return err
	}
	var d model.ManagedAccountExportDelivery
	if err = model.DB.First(&d, deliveryID).Error; err != nil {
		return err
	}
	var run model.ManagedAccountExportRun
	if err = model.DB.Where("id = ? AND schedule_id = ?", d.RunID, plan.ID).First(&run).Error; err != nil {
		return err
	}
	if d.Status != "failed" && d.Status != "uncertain" {
		return model.ErrExportScheduleConflict
	}
	if d.Status == "uncertain" && !confirmed {
		return ErrInvalidSchedule
	}
	record, err := model.GetManagedUsageExport(run.TaskID)
	if err != nil {
		return err
	}
	if err = service.CheckScheduledExportOwner(record, true); err != nil {
		return err
	}
	if record.ExpiresAt <= time.Now().Unix() {
		return model.ErrExportScheduleConflict
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		guard := tx.Model(plan).Where("version = ? AND enabled = ? AND deleted_at = 0 AND active_run_id IN ?", plan.Version, true, []int64{0, run.ID}).Updates(map[string]any{"active_run_id": run.ID, "version": gorm.Expr("version + 1")})
		if guard.Error != nil {
			return guard.Error
		}
		if guard.RowsAffected != 1 {
			return model.ErrExportScheduleConflict
		}
		result := tx.Model(&d).Where("version = ? AND status = ?", version, d.Status).Updates(map[string]any{"status": "pending", "attempts": 0, "next_attempt_at": 0, "error_code": "", "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return model.ErrExportScheduleConflict
		}
		return tx.Model(&run).Updates(map[string]any{"status": "delivering", "finished_at": 0, "error_code": ""}).Error
	})
}
