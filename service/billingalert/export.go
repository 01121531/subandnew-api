package billingalert

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const billingAlertExportLimit = 1_000_000

func BillingAlertExportDirectory() string {
	if configured := os.Getenv("BILLING_ALERT_EXPORT_DIR"); configured != "" {
		return configured
	}
	return filepath.Join("exports", "billing-alerts")
}

func ListAlertExports(actorID int, includeAll bool) ([]*model.BillingAlertExport, error) {
	query := model.DB.Order("created_at DESC, id DESC").Limit(200)
	if !includeAll {
		query = query.Where("actor_id = ?", actorID)
	}
	var exports []*model.BillingAlertExport
	err := query.Find(&exports).Error
	return exports, err
}

func GetAlertExport(taskID string, actorID int, includeAll bool) (*model.BillingAlertExport, error) {
	query := model.DB.Where("task_id = ?", taskID)
	if !includeAll {
		query = query.Where("actor_id = ?", actorID)
	}
	var record model.BillingAlertExport
	if err := query.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBillingNotFound
		}
		return nil, err
	}
	return &record, nil
}

func RunAlertExport(ctx context.Context, exportID int64) error {
	var record model.BillingAlertExport
	if err := model.DB.First(&record, exportID).Error; err != nil {
		return err
	}
	var filter AlertRecordFilter
	if err := json.Unmarshal([]byte(record.Query), &filter); err != nil {
		return finishAlertExport(&record, "failed", "invalid_export_query", "", 0, 0)
	}
	now := common.GetTimestamp()
	if err := model.DB.Model(&record).Updates(map[string]any{
		"status": "running", "started_at": now, "error_code": "", "updated_at": now,
	}).Error; err != nil {
		return err
	}
	directory := BillingAlertExportDirectory()
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return finishAlertExport(&record, "failed", "create_export_directory_failed", "", 0, 0)
	}
	taskSuffix := record.TaskID
	if len(taskSuffix) > 8 {
		taskSuffix = taskSuffix[len(taskSuffix)-8:]
	}
	fileName := fmt.Sprintf("billing-alerts-%s-%s.csv", time.Now().Format("20060102-150405"), taskSuffix)
	finalPath := filepath.Join(directory, fileName)
	temporaryPath := finalPath + ".part"
	file, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return finishAlertExport(&record, "failed", "open_export_file_failed", "", 0, 0)
	}
	defer os.Remove(temporaryPath)
	writer := csv.NewWriter(file)
	_ = writer.Write([]string{"来源", "事件类型", "实例", "系统类型", "规则", "监控范围", "指标", "条件", "观测值", "档位", "币种", "阈值", "美元消耗", "人民币账单", "折扣", "汇率", "汇率来源", "汇率日期", "收件人", "错误代码", "创建时间"})
	query := applyAlertRecordFilter(model.DB.Model(&model.BillingAlertEvent{}), filter).Order("created_at ASC, id ASC")
	var count int64
	for offset := 0; ; offset += 1000 {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return err
		}
		var events []*model.BillingAlertEvent
		if err := query.Offset(offset).Limit(1000).Find(&events).Error; err != nil {
			_ = file.Close()
			return finishAlertExport(&record, "failed", "query_export_records_failed", "", 0, 0)
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			discountRate, err := FormatDiscountPercent(event.DiscountRate)
			if err != nil {
				discountRate = event.DiscountRate
			}
			_ = writer.Write([]string{
				event.SourceType, event.EventType, event.InstanceName, event.InstanceKind, event.RuleName,
				event.ScopeMode, event.MetricKey, event.Conditions, event.ObservedValues, event.ThresholdName,
				event.Currency, event.Threshold, event.USDTotal, event.CNYTotal, discountRate,
				event.ExchangeRate, event.ExchangeSource, event.ExchangeObservedDate, event.Recipients,
				event.ErrorCode, time.Unix(event.CreatedAt, 0).Format(time.RFC3339),
			})
			count++
			if count > billingAlertExportLimit {
				writer.Flush()
				_ = file.Close()
				return finishAlertExport(&record, "failed", "export_too_large", "", 0, 0)
			}
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = file.Close()
		return finishAlertExport(&record, "failed", "write_export_failed", "", 0, 0)
	}
	if err := file.Close(); err != nil {
		return finishAlertExport(&record, "failed", "close_export_failed", "", 0, 0)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return finishAlertExport(&record, "failed", "finalize_export_failed", "", 0, 0)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return err
	}
	return finishAlertExport(&record, "succeeded", "", finalPath, count, info.Size())
}

func finishAlertExport(record *model.BillingAlertExport, status string, errorCode string, path string, count int64, size int64) error {
	now := common.GetTimestamp()
	updates := map[string]any{
		"status": status, "error_code": errorCode, "record_count": count,
		"file_size": size, "file_path": path, "finished_at": now, "updated_at": now,
	}
	if path != "" {
		updates["file_name"] = filepath.Base(path)
		updates["expires_at"] = now + int64((30 * 24 * time.Hour).Seconds())
	}
	if err := model.DB.Model(record).Updates(updates).Error; err != nil {
		return err
	}
	if status == "failed" {
		return errors.New(errorCode)
	}
	return nil
}

func CleanupExpiredAlertExports() error {
	now := common.GetTimestamp()
	var records []*model.BillingAlertExport
	if err := model.DB.Where("status = ? AND expires_at > 0 AND expires_at <= ?", "succeeded", now).Find(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		if record.FilePath != "" {
			_ = os.Remove(record.FilePath)
		}
		_ = model.DB.Model(record).Updates(map[string]any{"status": "expired", "file_path": "", "updated_at": now}).Error
	}
	matches, _ := filepath.Glob(filepath.Join(BillingAlertExportDirectory(), "*.part"))
	for _, path := range matches {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > time.Hour {
			_ = os.Remove(path)
		}
	}
	return nil
}

func AlertExportQueuePosition(record *model.BillingAlertExport) int64 {
	if record == nil || record.Status != "pending" {
		return 0
	}
	var count int64
	_ = model.DB.Model(&model.BillingAlertExport{}).Where("status = ? AND id < ?", "pending", record.ID).Count(&count).Error
	return count + 1
}
