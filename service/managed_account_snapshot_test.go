package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
)

func TestNormalizeManagedAccountRange(t *testing.T) {
	preset, err := NormalizeManagedAccountRange(7, 0, 0, "")
	require.NoError(t, err)
	require.Equal(t, "preset-7", preset.RangeKey)
	require.Equal(t, managedAccountDefaultTimezone, preset.Timezone)

	custom, err := NormalizeManagedAccountRange(0, 100, 200, "Asia/Shanghai")
	require.NoError(t, err)
	require.Contains(t, custom.RangeKey, "custom-")
	require.EqualValues(t, 100, custom.Start)
	require.EqualValues(t, 200, custom.End)

	_, err = NormalizeManagedAccountRange(3, 0, 0, "Asia/Shanghai")
	require.Error(t, err)
	_, err = NormalizeManagedAccountRange(0, 200, 100, "Asia/Shanghai")
	require.Error(t, err)
}

func TestManagedAccountFailedRefreshKeepsLastSuccess(t *testing.T) {
	truncate(t)
	accountRange, err := NormalizeManagedAccountRange(7, 0, 0, "Asia/Shanghai")
	require.NoError(t, err)
	success := &managedinstance.ObservationView{
		SourceInstanceID: 8,
		ObservedAt:       1234,
		CollectionStatus: model.ManagedInstanceCollectionSucceeded,
		ETag:             "etag-success",
		Data:             map[string]any{"total": 2.0},
	}
	require.NoError(t, saveManagedAccountSnapshot("", "", 8, model.ManagedAccountSnapshotKindOutput, accountRange, success, nil))
	require.NoError(t, saveManagedAccountSnapshot("", "", 8, model.ManagedAccountSnapshotKindOutput, accountRange, nil, errors.New("remote failed")))

	snapshot, err := findManagedAccountSnapshot(8, model.ManagedAccountSnapshotKindOutput, accountRange.RangeKey)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.EqualValues(t, 1234, snapshot.ObservedAt)
	require.Equal(t, "etag-success", snapshot.ETag)
	require.JSONEq(t, `{"total":2}`, snapshot.Payload)
	require.Equal(t, model.ManagedInstanceCollectionFailed, snapshot.LastAttemptStatus)
	require.Equal(t, "collection_failed", snapshot.LastErrorCode)
}

func TestManagedAccountFailedRefreshRetriesAfterOneMinute(t *testing.T) {
	now := int64(10_000)
	failed := &model.ManagedAccountSnapshot{
		LastAttemptAt:     now - int64(managedAccountFailureCooldown/time.Second),
		LastAttemptStatus: model.ManagedInstanceCollectionFailed,
	}
	require.True(t, managedAccountSectionNeedsRefresh(failed, now))

	failed.LastAttemptAt++
	require.False(t, managedAccountSectionNeedsRefresh(failed, now))

	succeeded := &model.ManagedAccountSnapshot{
		LastAttemptAt:     now - int64(managedAccountFailureCooldown/time.Second),
		LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}
	require.False(t, managedAccountSectionNeedsRefresh(succeeded, now))
}

func TestManagedAccountRefreshCooldownAndDeduplication(t *testing.T) {
	truncate(t)
	instance := &model.ManagedInstance{Name: "cached-new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://cached.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	accountRange, err := NormalizeManagedAccountRange(7, 0, 0, "Asia/Shanghai")
	require.NoError(t, err)
	now := common.GetTimestamp()
	for _, item := range []struct {
		kind string
		key  string
	}{
		{model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey},
		{model.ManagedAccountSnapshotKindOutput, accountRange.RangeKey},
	} {
		require.NoError(t, model.DB.Create(&model.ManagedAccountSnapshot{
			InstanceID: instance.Id, SnapshotKind: item.kind, RangeKey: item.key,
			Timezone: "Asia/Shanghai", Payload: `{}`, ObservedAt: now,
			LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
		}).Error)
	}

	fresh, err := EnqueueManagedAccountRefresh(instance.Id, 1, accountRange, false)
	require.NoError(t, err)
	require.False(t, fresh.Enqueued)
	require.Nil(t, fresh.Task)

	forced, err := EnqueueManagedAccountRefresh(instance.Id, 1, accountRange, true)
	require.NoError(t, err)
	require.True(t, forced.Enqueued)
	require.NotNil(t, forced.Task)

	duplicate, err := EnqueueManagedAccountRefresh(instance.Id, 1, accountRange, true)
	require.NoError(t, err)
	require.False(t, duplicate.Enqueued)
	require.NotNil(t, duplicate.Task)
	require.Equal(t, forced.Task.TaskID, duplicate.Task.TaskID)
}

func TestManagedAccountInventoryBackfillsLegacySnapshot(t *testing.T) {
	truncate(t)
	instance := &model.ManagedInstance{Name: "legacy-account-cache", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://legacy.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	require.NoError(t, model.DB.Create(&model.ManagedInstanceSnapshot{
		InstanceId: instance.Id, SnapshotType: model.ManagedInstanceSnapshotTypeInventory,
		ResourceKind: "account", ObservedAt: 5678, ETag: "legacy-etag", Payload: `{"resource_kind":"account","items":[],"total":0}`,
		CollectionStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)

	accountRange, err := NormalizeManagedAccountRange(7, 0, 0, "Asia/Shanghai")
	require.NoError(t, err)
	view, err := GetManagedAccountSnapshot(instance.Id, accountRange)
	require.NoError(t, err)
	require.NotNil(t, view.Inventory.Observation)
	require.EqualValues(t, 5678, view.Inventory.Observation.ObservedAt)
	require.Equal(t, "legacy-etag", view.Inventory.Observation.ETag)
}

func TestEnqueueManagedAccountExportFreezesSelectedInventory(t *testing.T) {
	truncate(t)
	instance := &model.ManagedInstance{Name: "export-source", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://export.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	page := managedinstance.InventoryPage{
		ResourceKind: "account", Total: 1,
		Items:   []managedinstance.InventoryItem{{ID: 6822196335042536000, IDText: "6822196335042536000", Name: "selected", Email: "selected@example.com", SourceID: "9"}},
		Sources: []managedinstance.InventorySource{{ID: "9", Name: "worker-nine"}},
	}
	payload, err := json.Marshal(page)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ManagedAccountSnapshot{
		InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
		RangeKey: managedAccountInventoryRangeKey, Timezone: managedAccountDefaultTimezone,
		ObservedAt: 100, Payload: string(payload), LastAttemptAt: 100,
		LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)

	task, err := EnqueueManagedAccountExport(7, ManagedAccountExportRequest{
		Source: "inventory", Locale: "zh-CN",
		Window:         managedinstance.TimeWindow{Start: 1786032000, End: 1786723199, Timezone: "Asia/Shanghai"},
		Items:          []ManagedAccountExportItemInput{{InstanceID: instance.Id, AccountID: "6822196335042536000"}},
		FilterSnapshot: json.RawMessage(`{"match_mode":"all","rules":[{"field":"email","operator":"contains","values":["example.com"],"value_mode":"any"}]}`),
	})
	require.NoError(t, err)
	items, err := model.ListManagedExportItems(task.TaskID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	var frozen managedinstance.AccountExportSelection
	require.NoError(t, json.Unmarshal([]byte(items[0].Metadata), &frozen))
	require.Equal(t, "selected@example.com", frozen.Account.Email)
	require.Equal(t, "worker-nine", frozen.SourceName)
	export, err := model.GetManagedUsageExport(task.TaskID)
	require.NoError(t, err)
	var snapshot managedAccountExportSnapshot
	require.NoError(t, json.Unmarshal([]byte(export.Query), &snapshot))
	require.JSONEq(t, `{"match_mode":"all","rules":[{"field":"email","operator":"contains","values":["example.com"],"value_mode":"any"}]}`, string(snapshot.FilterSnapshot))
}

func TestEnqueueManagedAccountExportUsesSelectedOutputSnapshot(t *testing.T) {
	truncate(t)
	instance := &model.ManagedInstance{Name: "output-source", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://output.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	inventoryPayload, err := json.Marshal(managedinstance.InventoryPage{
		ResourceKind: "account", Total: 1,
		Items: []managedinstance.InventoryItem{{ID: 2, IDText: "2", Name: "latest-inventory"}},
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ManagedAccountSnapshot{
		InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
		RangeKey: managedAccountInventoryRangeKey, Timezone: managedAccountDefaultTimezone,
		ObservedAt: 200, Payload: string(inventoryPayload), LastAttemptAt: 200,
		LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)
	outputPayload, err := json.Marshal(managedinstance.AccountOutputResult{
		SourceInstanceID: instance.Id,
		Items: []managedinstance.AccountOutputItem{{
			Account: managedinstance.InventoryItem{ID: 1, IDText: "1", Name: "selected-output", Email: "selected@example.com"},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ManagedAccountSnapshot{
		InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindOutput,
		RangeKey: "preset-30", PresetDays: 30, Timezone: managedAccountDefaultTimezone,
		ObservedAt: 150, Payload: string(outputPayload), LastAttemptAt: 150,
		LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)

	task, err := EnqueueManagedAccountExport(7, ManagedAccountExportRequest{
		Source: "account_output", RangeKey: "preset-30", Locale: "zh-CN",
		Window: managedinstance.TimeWindow{Start: 1786032000, End: 1786723199, Timezone: "Asia/Shanghai"},
		Items:  []ManagedAccountExportItemInput{{InstanceID: instance.Id, AccountID: "1"}},
	})
	require.NoError(t, err)
	items, err := model.ListManagedExportItems(task.TaskID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	var frozen managedinstance.AccountExportSelection
	require.NoError(t, json.Unmarshal([]byte(items[0].Metadata), &frozen))
	require.Equal(t, "selected-output", frozen.Account.Name)
	require.Equal(t, "selected@example.com", frozen.Account.Email)
}

func TestManagedAccountStandardSyncDueRequiresEveryPreset(t *testing.T) {
	instance := &model.ManagedInstance{Id: 9, Kind: model.ManagedInstanceKindSub2API}
	now := int64(10_000)
	latest := map[string]int64{
		managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey): now,
	}
	require.True(t, managedAccountStandardSyncDue(instance, latest, now, 3_600))
	for _, days := range managedAccountPresetDays {
		latest[managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindOutput, "preset-"+strconv.Itoa(days))] = now
	}
	require.False(t, managedAccountStandardSyncDue(instance, latest, now, 3_600))
	require.True(t, managedAccountStandardSyncDue(instance, latest, now+3_600, 3_600))

	conductor := &model.ManagedInstance{Id: 10, Kind: model.ManagedInstanceKindConductor}
	conductorLatest := map[string]int64{
		managedAccountScheduleKey(conductor.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey): now,
	}
	require.True(t, managedAccountStandardSyncDue(conductor, conductorLatest, now, 3_600))
	for _, days := range managedAccountPresetDays {
		conductorLatest[managedAccountScheduleKey(conductor.Id, model.ManagedAccountSnapshotKindOutput, "preset-"+strconv.Itoa(days))] = now
	}
	require.False(t, managedAccountStandardSyncDue(conductor, conductorLatest, now, 3_600))
}

func TestManagedAccountRunningTaskCanBeRequeued(t *testing.T) {
	truncate(t)
	payload := ManagedAccountSyncPayload{
		InstanceID: 77,
		Mode:       managedAccountStandardTaskMode,
		Range:      ManagedAccountRange{RangeKey: "preset-7", PresetDays: 7, Timezone: managedAccountDefaultTimezone},
	}
	task, created, err := EnqueueScopedSystemTask(model.SystemTaskTypeManagedAccountSync, "77:standard", payload, nil)
	require.NoError(t, err)
	require.True(t, created)
	claimed, ok, err := model.ClaimScopedSystemTask(task.ID, task.Type, "account-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, model.RequeueSystemTask(claimed.TaskID, "account-runner"))

	reloaded, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.SystemTaskStatusPending, reloaded.Status)
	require.Empty(t, reloaded.LockedBy)
	var lockCount int64
	require.NoError(t, model.DB.Model(&model.SystemTaskScopeLock{}).Where("task_id = ?", task.TaskID).Count(&lockCount).Error)
	require.Zero(t, lockCount)

}
