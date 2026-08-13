package managedinstance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

var ErrInstanceConnectionFailed = errors.New("instance_connection_failed")

const dataReadProbeCooldown = 5 * time.Second

type dataReadProbeState struct {
	mu          sync.Mutex
	lastAttempt time.Time
}

var dataReadProbeStates sync.Map

// EnsureDataConnection immediately rechecks an unhealthy instance before a data read.
// The short cooldown coalesces the parallel requests made when a data page opens.
func EnsureDataConnection(ctx context.Context, instanceID int64, actorID int) error {
	if instanceID <= 0 {
		return ErrInvalidInstance
	}
	instance, err := dataReadInstance(instanceID)
	if err != nil {
		return err
	}
	if instance.Status == model.ManagedInstanceStatusHealthy && instance.ConsecutiveFailures == 0 {
		return nil
	}

	stateValue, _ := dataReadProbeStates.LoadOrStore(instanceID, &dataReadProbeState{})
	state := stateValue.(*dataReadProbeState)
	state.mu.Lock()
	defer state.mu.Unlock()

	instance, err = dataReadInstance(instanceID)
	if err != nil {
		return err
	}
	if instance.Status == model.ManagedInstanceStatusHealthy && instance.ConsecutiveFailures == 0 {
		dataReadProbeStates.Delete(instanceID)
		return nil
	}
	if !state.lastAttempt.IsZero() && time.Since(state.lastAttempt) < dataReadProbeCooldown {
		return ErrInstanceConnectionFailed
	}
	if _, err = Probe(ctx, instanceID, actorID); err != nil {
		state.lastAttempt = time.Now()
		return fmt.Errorf("%w: %v", ErrInstanceConnectionFailed, err)
	}
	dataReadProbeStates.Delete(instanceID)
	return nil
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
