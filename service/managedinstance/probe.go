package managedinstance

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

func Probe(ctx context.Context, instanceID int64, actorID int) (*ProbeResult, error) {
	return probe(ctx, instanceID, actorID, nil)
}

func ProbeWithCommitGuard(ctx context.Context, instanceID int64, actorID int, guard CommitGuard) (*ProbeResult, error) {
	return probe(ctx, instanceID, actorID, guard)
}

func probe(ctx context.Context, instanceID int64, actorID int, guard CommitGuard) (*ProbeResult, error) {
	if instanceID <= 0 {
		return nil, ErrInvalidInstance
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, instanceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}
	credential, err := loadCredential(instanceID)
	if err != nil {
		var probeError *ProbeError
		if errors.As(err, &probeError) {
			if recordErr := recordProbeFailure(&instance, actorID, probeError, common.GetTimestamp(), guard); recordErr != nil {
				return nil, recordErr
			}
		}
		return nil, err
	}
	policy, err := ConnectorPolicyFromEnvironment()
	if err != nil {
		return nil, err
	}
	connector, err := NewConnector(&instance, policy)
	if err != nil {
		return nil, err
	}
	adapter, err := adapterForKind(instance.Kind)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now()
	result, probeErr := adapter.Probe(ctx, connector, credential)
	checkedAt := common.GetTimestamp()
	if probeErr != nil {
		if err := recordProbeFailure(&instance, actorID, probeErr, checkedAt, guard); err != nil {
			return nil, err
		}
		return nil, probeErr
	}
	result.LatencyMS = time.Since(startedAt).Milliseconds()
	result.CheckedAt = checkedAt
	capabilities, err := json.Marshal(result.Capabilities)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if guard != nil {
			if err := guard(tx); err != nil {
				return err
			}
		}
		if err := resolveProbeAlerts(tx, &instance, checkedAt); err != nil {
			return err
		}
		updates := map[string]any{
			"kind": result.Kind, "version": result.Version, "capabilities": string(capabilities),
			"status": model.ManagedInstanceStatusHealthy, "last_seen_at": checkedAt,
			"last_checked_at": checkedAt, "consecutive_failures": 0, "updated_at": checkedAt,
		}
		if err := tx.Model(&model.ManagedInstance{}).Where("id = ?", instanceID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ManagedInstanceCredential{}).Where("instance_id = ?", instanceID).
			Updates(map[string]any{"last_verified_at": checkedAt, "updated_at": checkedAt}).Error; err != nil {
			return err
		}
		return writeAuditOutcome(tx, instanceID, actorID, "check", "succeeded", map[string]any{
			"kind": result.Kind, "version": result.Version, "latency_ms": result.LatencyMS,
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func loadCredential(instanceID int64) (*CredentialMaterial, error) {
	var credential model.ManagedInstanceCredential
	if err := model.DB.Where("instance_id = ?", instanceID).First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if credential.ExpiresAt > 0 && credential.ExpiresAt <= common.GetTimestamp() {
		return nil, &ProbeError{Code: ProbeErrorCredentialExpired}
	}
	cipher, err := NewCredentialCipherFromEnvironment()
	if err != nil {
		return nil, err
	}
	payload, err := cipher.Decrypt(instanceID, credential.AuthType, credential.KeyVersion, credential.Ciphertext)
	if err != nil {
		return nil, err
	}
	return &CredentialMaterial{
		AuthType: credential.AuthType, AccessScope: normalizedAccessScope(credential.AccessScope),
		Secret: payload.Secret, UserID: payload.UserID,
	}, nil
}

func recordProbeFailure(instance *model.ManagedInstance, actorID int, probeErr error, checkedAt int64, guard CommitGuard) error {
	status := model.ManagedInstanceStatusOffline
	errorCode := "connector_failed"
	var typedError *ProbeError
	if errors.As(probeErr, &typedError) {
		errorCode = typedError.Code
		switch typedError.Code {
		case ProbeErrorAuthentication, ProbeErrorCredentialExpired, ProbeErrorPermission:
			status = model.ManagedInstanceStatusAuthFailed
		case ProbeErrorCompliance, ProbeErrorInvalidResponse:
			status = model.ManagedInstanceStatusDegraded
		}
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if guard != nil {
			if err := guard(tx); err != nil {
				return err
			}
		}
		nextFailures := instance.ConsecutiveFailures + 1
		if err := tx.Model(&model.ManagedInstance{}).Where("id = ?", instance.Id).Updates(map[string]any{
			"status": status, "last_checked_at": checkedAt,
			"consecutive_failures": gorm.Expr("consecutive_failures + ?", 1), "updated_at": checkedAt,
		}).Error; err != nil {
			return err
		}
		if err := reconcileProbeFailureAlert(tx, instance, status, errorCode, checkedAt, nextFailures); err != nil {
			return err
		}
		return writeAuditOutcome(tx, instance.Id, actorID, "check", "failed", map[string]any{"error_code": errorCode})
	})
}
