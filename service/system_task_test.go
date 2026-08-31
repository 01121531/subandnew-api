package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withSystemTaskRegistry swaps the package registry for the given handlers for
// the duration of a test and restores the original registry afterward.
func withSystemTaskRegistry(t *testing.T, handlers ...SystemTaskHandler) {
	t.Helper()
	systemTaskHandlersMu.Lock()
	saved := systemTaskHandlers
	systemTaskHandlers = map[string]SystemTaskHandler{}
	for _, h := range handlers {
		systemTaskHandlers[h.Type()] = h
	}
	systemTaskHandlersMu.Unlock()
	t.Cleanup(func() {
		systemTaskHandlersMu.Lock()
		systemTaskHandlers = saved
		systemTaskHandlersMu.Unlock()
	})
}

type stubScheduledHandler struct {
	taskType string
	enabled  bool
	interval time.Duration
	onRun    func(ctx context.Context, task *model.SystemTask, runnerID string)
}

type stubSystemTaskRunResult struct {
	taskID   string
	taskType string
	err      error
}

func (h *stubScheduledHandler) Type() string { return h.taskType }

func (h *stubScheduledHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if h.onRun != nil {
		h.onRun(ctx, task, runnerID)
	}
}

func (h *stubScheduledHandler) Enabled() bool           { return h.enabled }
func (h *stubScheduledHandler) Interval() time.Duration { return h.interval }
func (h *stubScheduledHandler) NewPayload() any         { return nil }

func countSystemTasks(t *testing.T, taskType string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Where("type = ?", taskType).Count(&count).Error)
	return count
}

func TestManagedInstanceProbeSchedulerUsesScopedTasksAndBackoff(t *testing.T) {
	truncate(t)
	now := common.GetTimestamp()
	due := &model.ManagedInstance{
		Name: "due", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://due.example.com",
		Environment: "production", TLSVerify: true, CheckIntervalSeconds: 60,
	}
	notDue := &model.ManagedInstance{
		Name: "not-due", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://not-due.example.com",
		Environment: "production", TLSVerify: true, CheckIntervalSeconds: 60, LastCheckedAt: now,
	}
	require.NoError(t, model.DB.Create(due).Error)
	require.NoError(t, model.DB.Create(notDue).Error)

	scheduleDueManagedInstanceProbes(now)
	scheduleDueManagedInstanceProbes(now)

	var tasks []*model.SystemTask
	require.NoError(t, model.DB.Where("type = ?", model.SystemTaskTypeManagedInstanceProbe).Find(&tasks).Error)
	require.Len(t, tasks, 1)
	require.Equal(t, fmt.Sprintf("%d", due.Id), tasks[0].ScopeKey)
}

func TestManagedInstanceSchedulerScansBeyondFirstBatch(t *testing.T) {
	truncate(t)
	instances := make([]model.ManagedInstance, 0, 501)
	for index := 0; index < 501; index++ {
		instances = append(instances, model.ManagedInstance{
			Name: fmt.Sprintf("instance-%03d", index), Kind: model.ManagedInstanceKindNewAPI,
			BaseURL: fmt.Sprintf("https://instance-%03d.example.com", index), Environment: "production", TLSVerify: true,
		})
	}
	require.NoError(t, model.DB.CreateInBatches(&instances, 100).Error)

	visited := 0
	batchCount := 0
	forEachManagedInstanceBatch(func(batch []*model.ManagedInstance) bool {
		batchCount++
		visited += len(batch)
		return true
	})
	require.Equal(t, 2, batchCount)
	require.Equal(t, 501, visited)
}

func TestRunWithLeaseHeartbeatHonorsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	returned := make(chan struct{})
	go func() {
		runWithLeaseHeartbeat(ctx, &model.SystemTask{TaskID: "cancelled-task"}, "runner", func(taskCtx context.Context) {
			<-taskCtx.Done()
		})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("lease heartbeat did not return after parent cancellation")
	}
}

func TestManagedInstanceProbeDueRetriesFailuresAfterOneHour(t *testing.T) {
	now := int64(20_000)
	instance := &model.ManagedInstance{
		Id: 4, CheckIntervalSeconds: 60, LastCheckedAt: now - 3599, ConsecutiveFailures: 1,
	}
	require.False(t, managedInstanceProbeDue(instance, now))
	instance.LastCheckedAt = now - 3600
	require.True(t, managedInstanceProbeDue(instance, now))
}

func TestManagedInstanceSyncSchedulerUsesLatestSummaryAndScopedTasks(t *testing.T) {
	truncate(t)
	now := common.GetTimestamp()
	due := &model.ManagedInstance{
		Name: "sync-due", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://sync-due.example.com",
		Environment: "production", TLSVerify: true,
	}
	notDue := &model.ManagedInstance{
		Name: "sync-not-due", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://sync-not-due.example.com",
		Environment: "production", TLSVerify: true,
	}
	require.NoError(t, model.DB.Create(due).Error)
	require.NoError(t, model.DB.Create(notDue).Error)
	require.NoError(t, model.DB.Create(&model.ManagedInstanceSnapshot{
		InstanceId: notDue.Id, SnapshotType: model.ManagedInstanceSnapshotTypeSummary,
		ResourceKind: "", ObservedAt: now, Payload: "null", CollectionStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)

	scheduleDueManagedInstanceSyncs(now)
	scheduleDueManagedInstanceSyncs(now)

	var tasks []*model.SystemTask
	require.NoError(t, model.DB.Where("type = ?", model.SystemTaskTypeManagedInstanceSync).Find(&tasks).Error)
	require.Len(t, tasks, 1)
	require.Equal(t, fmt.Sprintf("%d", due.Id), tasks[0].ScopeKey)
}

func TestSystemTaskSchedulerCreatesWhenDueAndDedups(t *testing.T) {
	truncate(t)

	handler := &stubScheduledHandler{taskType: "test_scheduled", enabled: true, interval: time.Minute}
	withSystemTaskRegistry(t, handler)

	runSystemTaskScheduler()
	require.Equal(t, int64(1), countSystemTasks(t, handler.taskType))

	// An active (pending) row already exists, so a second pass must not create
	// another row.
	runSystemTaskScheduler()
	require.Equal(t, int64(1), countSystemTasks(t, handler.taskType))

	// Finish the run; with a fresh updated_at the next run is not due yet.
	latest, err := model.GetLatestSystemTask(handler.taskType)
	require.NoError(t, err)
	require.NotNil(t, latest)
	_, claimed, err := model.ClaimSystemTask(latest.ID, handler.taskType, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.FinishSystemTask(latest.TaskID, "runner-a", model.SystemTaskStatusSucceeded, nil, ""))

	runSystemTaskScheduler()
	require.Equal(t, int64(1), countSystemTasks(t, handler.taskType))

	// Backdate the finished row beyond the interval -> the job becomes due again.
	require.NoError(t, model.DB.Model(&model.SystemTask{}).
		Where("task_id = ?", latest.TaskID).
		Update("updated_at", common.GetTimestamp()-120).Error)

	runSystemTaskScheduler()
	require.Equal(t, int64(2), countSystemTasks(t, handler.taskType))
}

func TestSystemTaskSchedulerSkipsDisabled(t *testing.T) {
	truncate(t)

	handler := &stubScheduledHandler{taskType: "test_disabled", enabled: false, interval: time.Minute}
	withSystemTaskRegistry(t, handler)

	runSystemTaskScheduler()
	assert.Equal(t, int64(0), countSystemTasks(t, handler.taskType))
}

func TestSystemTaskClaimPassDispatchesByType(t *testing.T) {
	truncate(t)

	ran := make(chan stubSystemTaskRunResult, 1)
	handler := &stubScheduledHandler{
		taskType: "test_dispatch",
		enabled:  true,
		interval: time.Minute,
		onRun: func(_ context.Context, task *model.SystemTask, runnerID string) {
			ran <- stubSystemTaskRunResult{
				taskType: task.Type,
				err:      model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, nil, ""),
			}
		},
	}
	withSystemTaskRegistry(t, handler)

	_, err := model.CreateSystemTask(handler.taskType, nil, nil)
	require.NoError(t, err)

	runSystemTaskClaimPass(context.Background(), "runner-dispatch")

	select {
	case got := <-ran:
		require.NoError(t, got.err)
		assert.Equal(t, handler.taskType, got.taskType)
	case <-time.After(2 * time.Second):
		t.Fatal("claimed task was not dispatched to its handler")
	}

	require.Eventually(t, func() bool {
		latest, err := model.GetLatestSystemTask(handler.taskType)
		return err == nil && latest != nil && latest.Status == model.SystemTaskStatusSucceeded
	}, 2*time.Second, 20*time.Millisecond)
}

func TestSystemTaskClaimPassDispatchesEarliestPendingByType(t *testing.T) {
	truncate(t)

	ran := make(chan stubSystemTaskRunResult, 2)
	handlerA := &stubScheduledHandler{
		taskType: "test_dispatch_a",
		enabled:  true,
		interval: time.Minute,
		onRun: func(_ context.Context, task *model.SystemTask, runnerID string) {
			ran <- stubSystemTaskRunResult{
				taskID: task.TaskID,
				err:    model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, nil, ""),
			}
		},
	}
	handlerB := &stubScheduledHandler{
		taskType: "test_dispatch_b",
		enabled:  true,
		interval: time.Minute,
		onRun: func(_ context.Context, task *model.SystemTask, runnerID string) {
			ran <- stubSystemTaskRunResult{
				taskID: task.TaskID,
				err:    model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, nil, ""),
			}
		},
	}
	withSystemTaskRegistry(t, handlerA, handlerB)

	firstA, err := model.CreateSystemTask(handlerA.taskType, nil, nil)
	require.NoError(t, err)
	secondTaskID, err := model.GenerateSystemTaskID()
	require.NoError(t, err)
	secondA := &model.SystemTask{
		TaskID: secondTaskID,
		Type:   handlerA.taskType,
		Status: model.SystemTaskStatusPending,
	}
	require.NoError(t, model.DB.Create(secondA).Error)
	firstB, err := model.CreateSystemTask(handlerB.taskType, nil, nil)
	require.NoError(t, err)

	runSystemTaskClaimPass(context.Background(), "runner-dispatch")

	got := map[string]bool{}
	for range 2 {
		select {
		case result := <-ran:
			require.NoError(t, result.err)
			got[result.taskID] = true
		case <-time.After(2 * time.Second):
			t.Fatal("claimed tasks were not dispatched to their handlers")
		}
	}

	assert.True(t, got[firstA.TaskID])
	assert.True(t, got[firstB.TaskID])
	assert.False(t, got[secondA.TaskID])

	require.Eventually(t, func() bool {
		reloaded, err := model.GetSystemTaskByTaskID(secondA.TaskID)
		return err == nil && reloaded != nil && reloaded.Status == model.SystemTaskStatusPending
	}, 2*time.Second, 20*time.Millisecond)
}

func TestSystemTaskClaimPassDoesNotClaimBeyondDispatchCapacity(t *testing.T) {
	truncate(t)

	started := make(chan struct{}, systemTaskDispatchLimit+4)
	finished := make(chan error, systemTaskDispatchLimit)
	release := make(chan struct{})
	handler := &stubScheduledHandler{
		taskType: "test_dispatch_capacity",
		onRun: func(_ context.Context, task *model.SystemTask, runnerID string) {
			started <- struct{}{}
			<-release
			finished <- model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, nil, "")
		},
	}
	withSystemTaskRegistry(t, handler)

	for index := 0; index < systemTaskDispatchLimit+4; index++ {
		_, _, err := EnqueueScopedSystemTask(handler.taskType, fmt.Sprintf("scope-%d", index), nil, nil)
		require.NoError(t, err)
	}

	runSystemTaskClaimPass(context.Background(), "runner-capacity")
	require.Eventually(t, func() bool { return len(started) == systemTaskDispatchLimit }, 2*time.Second, 20*time.Millisecond)

	var running int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Where("type = ? AND status = ?", handler.taskType, model.SystemTaskStatusRunning).Count(&running).Error)
	require.Equal(t, int64(systemTaskDispatchLimit), running)
	var pending int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Where("type = ? AND status = ?", handler.taskType, model.SystemTaskStatusPending).Count(&pending).Error)
	require.Equal(t, int64(4), pending)

	close(release)
	require.Eventually(t, func() bool { return len(finished) == systemTaskDispatchLimit }, 2*time.Second, 20*time.Millisecond)
	for index := 0; index < systemTaskDispatchLimit; index++ {
		require.NoError(t, <-finished)
	}
	require.Eventually(t, func() bool {
		var succeeded int64
		return model.DB.Model(&model.SystemTask{}).Where("type = ? AND status = ?", handler.taskType, model.SystemTaskStatusSucceeded).Count(&succeeded).Error == nil && succeeded == systemTaskDispatchLimit
	}, 2*time.Second, 20*time.Millisecond)
}

func TestEnqueueSystemTaskReportsCreatedAndExistingActive(t *testing.T) {
	truncate(t)

	first, created, err := EnqueueSystemTask("test_enqueue", map[string]bool{"manual": true})
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, first)

	existing, created, err := EnqueueSystemTask("test_enqueue", nil)
	require.NoError(t, err)
	require.False(t, created)
	require.NotNil(t, existing)
	assert.Equal(t, first.TaskID, existing.TaskID)

	_, claimed, err := model.ClaimSystemTask(first.ID, first.Type, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.FinishSystemTask(first.TaskID, "runner-a", model.SystemTaskStatusSucceeded, nil, ""))

	second, created, err := EnqueueSystemTask("test_enqueue", nil)
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, second)
	assert.NotEqual(t, first.TaskID, second.TaskID)
}
