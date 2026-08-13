package managedinstance

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

var ErrInstanceConnectionFailed = errors.New("instance_connection_failed")
var ErrRemoteDataUnavailable = errors.New("remote_data_unavailable")

const dataReadProbeCooldown = 5 * time.Second

type dataReadProbeState struct {
	mu            sync.Mutex
	lastAttempt   time.Time
	lastSucceeded bool
}

var dataReadProbeStates sync.Map

// EnsureDataConnection immediately rechecks an unhealthy instance before a data read.
// The short cooldown coalesces the parallel requests made when a data page opens.
func EnsureDataConnection(ctx context.Context, instanceID int64, actorID int) error {
	return ensureDataConnection(ctx, instanceID, actorID, false)
}

// RecoverDataConnection forces one coalesced probe after a remote data call fails.
func RecoverDataConnection(ctx context.Context, instanceID int64, actorID int) error {
	return ensureDataConnection(ctx, instanceID, actorID, true)
}

func ensureDataConnection(ctx context.Context, instanceID int64, actorID int, force bool) error {
	if instanceID <= 0 {
		return ErrInvalidInstance
	}
	instance, err := dataReadInstance(instanceID)
	if err != nil {
		return err
	}
	if !force && instance.Status == model.ManagedInstanceStatusHealthy && instance.ConsecutiveFailures == 0 {
		return nil
	}

	stateValue, _ := dataReadProbeStates.LoadOrStore(instanceID, &dataReadProbeState{})
	state := stateValue.(*dataReadProbeState)
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.lastAttempt.IsZero() && time.Since(state.lastAttempt) < dataReadProbeCooldown {
		if state.lastSucceeded {
			return nil
		}
		return ErrInstanceConnectionFailed
	}

	instance, err = dataReadInstance(instanceID)
	if err != nil {
		return err
	}
	if !force && instance.Status == model.ManagedInstanceStatusHealthy && instance.ConsecutiveFailures == 0 {
		return nil
	}
	if _, err = Probe(ctx, instanceID, actorID); err != nil {
		state.lastAttempt = time.Now()
		state.lastSucceeded = false
		return fmt.Errorf("%w: %v", ErrInstanceConnectionFailed, err)
	}
	state.lastAttempt = time.Now()
	state.lastSucceeded = true
	return nil
}

func ShouldRecoverDataConnection(err error) bool {
	if err == nil || errors.Is(err, ErrInvalidInstance) || errors.Is(err, ErrUnsupportedCapability) || errors.Is(err, ErrUsageExportTooLarge) {
		return false
	}
	var probeError *ProbeError
	var networkError net.Error
	return errors.As(err, &probeError) ||
		errors.As(err, &networkError) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrConnectorTargetBlocked) ||
		errors.Is(err, ErrConnectorRedirect) ||
		errors.Is(err, ErrConnectorResponseLarge)
}

func RemoteDataError(err error) error {
	if ShouldRecoverDataConnection(err) {
		return fmt.Errorf("%w: %v", ErrRemoteDataUnavailable, err)
	}
	return err
}

func dataReadInstance(instanceID int64) (*model.ManagedInstance, error) {
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, instanceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}
	return &instance, nil
}
