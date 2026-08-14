package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/billingalert"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// systemTaskRunnerIdleInterval is the fallback poll interval used to pick up
	// tasks created on other nodes and mark expired leases failed.
	systemTaskRunnerIdleInterval = 15 * time.Second
	systemTaskLockTTL            = 60 * time.Second

	// systemTaskSchedulerInterval throttles how often the scheduler/stale-lock
	// pass runs, independent of how often the runner wakes to claim tasks.
	systemTaskSchedulerInterval = 15 * time.Second
	systemTaskStaleLockInterval = 30 * time.Second
)

// SystemTaskHandler executes a claimed task of a specific type. Run owns the
// task lifecycle from claim to terminal state: it MUST call
// model.FinishSystemTask (succeeded/failed) before returning and MUST honor
// ctx cancellation, which the runner triggers if the per-type lock is lost.
type SystemTaskHandler interface {
	Type() string
	Run(ctx context.Context, task *model.SystemTask, runnerID string)
}

// ScheduledSystemTaskHandler is a SystemTaskHandler that the scheduler also
// creates periodically when enabled and the configured interval has elapsed
// since the last run.
type ScheduledSystemTaskHandler interface {
	SystemTaskHandler
	Enabled() bool
	Interval() time.Duration
	NewPayload() any
}

var (
	systemTaskHandlersMu          sync.RWMutex
	systemTaskHandlers            = map[string]SystemTaskHandler{}
	managedUsageExportCleanupMu   sync.Mutex
	managedUsageExportLastCleanup time.Time
)

// RegisterSystemTaskHandler registers a handler keyed by its Type(). It must be
// called before StartSystemTaskRunner (or any time, since the runner snapshots
// the registry every pass). Re-registering a type replaces the previous handler.
func RegisterSystemTaskHandler(h SystemTaskHandler) {
	if h == nil {
		return
	}
	systemTaskHandlersMu.Lock()
	defer systemTaskHandlersMu.Unlock()
	systemTaskHandlers[h.Type()] = h
}

func registeredSystemTaskHandlers() []SystemTaskHandler {
	systemTaskHandlersMu.RLock()
	defer systemTaskHandlersMu.RUnlock()
	handlers := make([]SystemTaskHandler, 0, len(systemTaskHandlers))
	for _, h := range systemTaskHandlers {
		handlers = append(handlers, h)
	}
	return handlers
}

var (
	systemTaskRunnerOnce sync.Once
	systemTaskRunnerMu   sync.Mutex
	systemTaskRunnerStop context.CancelFunc
	systemTaskRunnerWG   sync.WaitGroup
	// systemTaskWakeup signals the runner to check for runnable tasks
	// immediately instead of waiting for the idle poll. Buffered so a signal
	// raised while the runner is busy is not lost and is handled on the next loop.
	systemTaskWakeup = make(chan struct{}, 1)
)

// notifySystemTaskRunner wakes the runner without blocking. If a wakeup is
// already pending it is a no-op, which is fine since one pass drains all work.
func notifySystemTaskRunner() {
	select {
	case systemTaskWakeup <- struct{}{}:
	default:
	}
}

func StartSystemTaskRunner() {
	systemTaskRunnerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		handlers := registeredSystemTaskHandlers()
		supportedTypes := make([]string, 0, len(handlers))
		for _, handler := range handlers {
			supportedTypes = append(supportedTypes, handler.Type())
		}
		retired, err := model.RetireUnsupportedSystemTasks(supportedTypes, common.GetTimestamp())
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("retire unsupported system tasks failed: %v", err))
		} else if retired > 0 {
			logger.LogInfo(context.Background(), fmt.Sprintf("retired %d unsupported active system tasks", retired))
		}

		runnerID := fmt.Sprintf("%s-%s", common.NodeName, common.GetRandomString(8))
		runnerCtx, cancel := context.WithCancel(context.Background())
		systemTaskRunnerMu.Lock()
		systemTaskRunnerStop = cancel
		systemTaskRunnerMu.Unlock()
		systemTaskRunnerWG.Add(1)
		gopool.Go(func() {
			defer systemTaskRunnerWG.Done()
			logger.LogInfo(context.Background(), fmt.Sprintf("system task runner started: runner=%s idle_interval=%s", runnerID, systemTaskRunnerIdleInterval))

			ticker := time.NewTicker(systemTaskRunnerIdleInterval)
			defer ticker.Stop()

			var lastScheduler time.Time
			var lastStaleLockCleanup time.Time
			runPass := func() {
				if runnerCtx.Err() != nil {
					return
				}
				// The scheduler/stale-lock pass is throttled independently of the
				// claim pass: on-demand wakeups should claim
				// immediately without re-running the scheduler every time.
				now := time.Now()
				if now.Sub(lastStaleLockCleanup) >= systemTaskStaleLockInterval {
					lastStaleLockCleanup = now
					if err := model.ExpireStaleSystemTaskLocks(common.GetTimestamp()); err != nil {
						logger.LogWarn(context.Background(), fmt.Sprintf("system task stale lock cleanup failed: %v", err))
					}
				}
				if now.Sub(lastScheduler) >= systemTaskSchedulerInterval {
					lastScheduler = now
					runSystemTaskScheduler()
				}
				runSystemTaskClaimPass(runnerCtx, runnerID)
			}

			runPass()
			for {
				select {
				case <-runnerCtx.Done():
					return
				case <-ticker.C:
				case <-systemTaskWakeup:
				}
				runPass()
			}
		})
	})
}

func StopSystemTaskRunner(ctx context.Context) error {
	systemTaskRunnerMu.Lock()
	cancel := systemTaskRunnerStop
	systemTaskRunnerMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	done := make(chan struct{})
	go func() {
		systemTaskRunnerWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// EnqueueSystemTask creates an on-demand task of the given type. The returned
// bool is true only when a new pending row was created; false means an active
// task of the same type already exists and was returned.
func EnqueueSystemTask(taskType string, payload any) (*model.SystemTask, bool, error) {
	activeTask, err := model.GetActiveSystemTask(taskType)
	if err != nil {
		return nil, false, err
	}
	if activeTask != nil {
		return activeTask, false, nil
	}

	task, err := model.CreateSystemTask(taskType, payload, nil)
	if err != nil {
		activeTask, activeErr := model.GetActiveSystemTask(taskType)
		if activeErr == nil && activeTask != nil {
			return activeTask, false, nil
		}
		return nil, false, err
	}
	notifySystemTaskRunner()
	return task, true, nil
}

// EnqueueQueuedSystemTask adds a task without an ActiveKey so multiple users
// can queue work of the same type. The per-type lease serializes execution.
func EnqueueQueuedSystemTask(taskType string, payload any, state any) (*model.SystemTask, error) {
	task, err := model.CreateQueuedSystemTask(taskType, payload, state)
	if err != nil {
		return nil, err
	}
	notifySystemTaskRunner()
	return task, nil
}

// EnqueueScopedSystemTask deduplicates work by type and scope while allowing
// different scopes to execute concurrently.
func EnqueueScopedSystemTask(taskType string, scopeKey string, payload any, state any) (*model.SystemTask, bool, error) {
	activeTask, err := model.GetActiveScopedSystemTask(taskType, scopeKey)
	if err != nil {
		return nil, false, err
	}
	if activeTask != nil {
		return activeTask, false, nil
	}
	task, err := model.CreateScopedSystemTask(taskType, scopeKey, payload, state)
	if err != nil {
		activeTask, activeErr := model.GetActiveScopedSystemTask(taskType, scopeKey)
		if activeErr == nil && activeTask != nil {
			return activeTask, false, nil
		}
		return nil, false, err
	}
	notifySystemTaskRunner()
	return task, true, nil
}

// runSystemTaskClaimPass dispatches unscoped work under its per-type lease and
// scoped work under an independent type+scope lease.
func runSystemTaskClaimPass(ctx context.Context, runnerID string) {
	handlers := registeredSystemTaskHandlers()
	handlersByType := make(map[string]SystemTaskHandler, len(handlers))
	taskTypes := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		taskTypes = append(taskTypes, handler.Type())
		handlersByType[handler.Type()] = handler
	}
	pendingTasks, err := model.FindPendingSystemTasksByTypes(taskTypes, 100)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task runner query failed: %v", err))
		return
	}
	for _, task := range pendingTasks {
		if ctx.Err() != nil {
			return
		}
		handler := handlersByType[task.Type]
		if handler == nil {
			continue
		}
		var claimedTask *model.SystemTask
		var claimed bool
		if task.ScopeKey == "" {
			claimedTask, claimed, err = model.ClaimSystemTask(task.ID, handler.Type(), runnerID, systemTaskLockUntil())
		} else {
			claimedTask, claimed, err = model.ClaimScopedSystemTask(task.ID, handler.Type(), runnerID, systemTaskLockUntil())
		}
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("system task claim failed: %v", err))
			continue
		}
		if !claimed {
			continue
		}
		dispatchHandler := handler
		dispatchTask := claimedTask
		systemTaskRunnerWG.Add(1)
		gopool.Go(func() {
			defer systemTaskRunnerWG.Done()
			runWithLeaseHeartbeat(ctx, dispatchTask, runnerID, func(taskCtx context.Context) {
				dispatchHandler.Run(taskCtx, dispatchTask, runnerID)
			})
			notifySystemTaskRunner()
		})
	}
}

// runSystemTaskScheduler creates a new task row for each enabled scheduled
// handler whose interval has elapsed since its last run and that has no active
// row. The task active_key unique index deduplicates concurrent creation while
// the per-type lock guarantees only one runner executes the task.
func runSystemTaskScheduler() {
	managedUsageExportCleanupMu.Lock()
	if time.Since(managedUsageExportLastCleanup) >= time.Hour {
		if err := CleanupExpiredManagedUsageExports(); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed usage export cleanup failed: %v", err))
		} else {
			managedUsageExportLastCleanup = time.Now()
		}
		if err := billingalert.CleanupExpiredAlertExports(); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("billing alert export cleanup failed: %v", err))
		}
		if err := cleanupManagedAccountCustomSnapshots(common.GetTimestamp()); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed account snapshot cleanup failed: %v", err))
		}
	}
	managedUsageExportCleanupMu.Unlock()
	now := common.GetTimestamp()
	handlers := registeredSystemTaskHandlers()
	scheduledHandlers := make([]ScheduledSystemTaskHandler, 0, len(handlers))
	taskTypes := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		scheduled, ok := handler.(ScheduledSystemTaskHandler)
		if !ok || !scheduled.Enabled() {
			continue
		}
		scheduledHandlers = append(scheduledHandlers, scheduled)
		taskTypes = append(taskTypes, scheduled.Type())
	}
	latestTasks, err := model.GetLatestSystemTasks(taskTypes)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler query failed: %v", err))
		return
	}
	for _, scheduled := range scheduledHandlers {
		latest := latestTasks[scheduled.Type()]
		if latest != nil {
			if latest.Status == model.SystemTaskStatusPending || latest.Status == model.SystemTaskStatusRunning {
				continue // an active row already exists
			}
			if now-latest.UpdatedAt < int64(scheduled.Interval().Seconds()) {
				continue // not due yet
			}
		}
		if _, err := model.CreateSystemTask(scheduled.Type(), scheduled.NewPayload(), nil); err != nil {
			activeTask, activeErr := model.GetActiveSystemTask(scheduled.Type())
			if activeErr == nil && activeTask != nil {
				continue
			}
			if activeErr != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler active lookup failed: type=%s err=%v", scheduled.Type(), activeErr))
			}
			logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler create failed: type=%s err=%v", scheduled.Type(), err))
			continue
		}
	}
	scheduleDueManagedInstanceProbes(now)
	scheduleDueManagedInstanceSyncs(now)
	scheduleDueManagedAccountSyncs(now)
	enqueueDueManagedInstanceAlertEmails(now)
	resumeManagedInstanceOperationBatches()
}

// runWithLeaseHeartbeat renews the per-type lock on a background ticker while
// fn runs. The TTL is a crash-detection window, not a task time limit: an
// arbitrarily long handler stays alive as long as the heartbeat succeeds.
func runWithLeaseHeartbeat(parent context.Context, task *model.SystemTask, runnerID string, fn func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	interval := systemTaskLockTTL / 3
	if interval <= 0 {
		interval = systemTaskLockTTL
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := model.RenewSystemTaskLock(task.TaskID, runnerID, systemTaskLockUntil()); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	fn(ctx)
	close(done)
}

func systemTaskLockUntil() int64 {
	return common.GetTimestamp() + int64(systemTaskLockTTL.Seconds())
}
