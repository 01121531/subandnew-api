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
	require.Equal(t, managedAccountSnapshotSchema, snapshot.SchemaVersion)
}

func TestManagedAccountDailySnapshotKeepsFirstSuccessfulStandardCapture(t *testing.T) {
	truncate(t)
	location := time.FixedZone(managedAccountDefaultTimezone, 8*60*60)
	firstObservedAt := time.Date(2026, time.September, 3, 0, 0, 5, 0, location).Unix()
	accountRange, err := NormalizeManagedAccountRange(7, 0, 0, managedAccountDefaultTimezone)
	require.NoError(t, err)

	first := &managedinstance.ObservationView{
		SourceInstanceID: 8, ObservedAt: firstObservedAt,
		CollectionStatus: model.ManagedInstanceCollectionSucceeded,
		Data:             map[string]any{"total": 1},
	}
	require.NoError(t, saveManagedAccountSnapshot("", "", 8, model.ManagedAccountSnapshotKindOutput, accountRange, first, nil))

	second := &managedinstance.ObservationView{
		SourceInstanceID: 8, ObservedAt: firstObservedAt + int64((12*time.Hour)/time.Second),
		CollectionStatus: model.ManagedInstanceCollectionSucceeded,
		Data:             map[string]any{"total": 2},
	}
	require.NoError(t, saveManagedAccountSnapshot("", "", 8, model.ManagedAccountSnapshotKindOutput, accountRange, second, nil))

	var archive model.ManagedAccountDailySnapshot
	require.NoError(t, model.DB.Where(
		"instance_id = ? AND snapshot_kind = ? AND range_key = ? AND snapshot_date = ?",
		8, model.ManagedAccountSnapshotKindOutput, accountRange.RangeKey, "2026-09-03",
	).First(&archive).Error)
	require.EqualValues(t, firstObservedAt, archive.ObservedAt)
	require.EqualValues(t, time.Date(2026, time.September, 3, 0, 0, 0, 0, location).Unix(), archive.BoundaryAt)
	require.JSONEq(t, `{"total":1}`, archive.Payload)

	nextDay := &managedinstance.ObservationView{
		SourceInstanceID: 8, ObservedAt: time.Date(2026, time.September, 4, 0, 0, 4, 0, location).Unix(),
		CollectionStatus: model.ManagedInstanceCollectionSucceeded,
		Data:             map[string]any{"total": 3},
	}
	require.NoError(t, saveManagedAccountSnapshot("", "", 8, model.ManagedAccountSnapshotKindOutput, accountRange, nextDay, nil))
	var count int64
	require.NoError(t, model.DB.Model(&model.ManagedAccountDailySnapshot{}).Where("instance_id = ?", 8).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestManagedAccountDailySnapshotSkipsCustomRangesAndFailures(t *testing.T) {
	truncate(t)
	customRange, err := NormalizeManagedAccountRange(0, 100, 200, managedAccountDefaultTimezone)
	require.NoError(t, err)
	observation := &managedinstance.ObservationView{
		SourceInstanceID: 8, ObservedAt: time.Date(2026, time.September, 3, 0, 0, 5, 0, time.FixedZone(managedAccountDefaultTimezone, 8*60*60)).Unix(),
		CollectionStatus: model.ManagedInstanceCollectionSucceeded,
		Data:             map[string]any{"total": 1},
	}
	require.NoError(t, saveManagedAccountSnapshot("", "", 8, model.ManagedAccountSnapshotKindOutput, customRange, observation, nil))

	presetRange, err := NormalizeManagedAccountRange(7, 0, 0, managedAccountDefaultTimezone)
	require.NoError(t, err)
	require.NoError(t, saveManagedAccountSnapshot("", "", 8, model.ManagedAccountSnapshotKindOutput, presetRange, nil, errors.New("remote failed")))

	var count int64
	require.NoError(t, model.DB.Model(&model.ManagedAccountDailySnapshot{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestManagedAccountFailedRefreshDoesNotAdvancePayloadSchema(t *testing.T) {
	truncate(t)
	accountRange, err := NormalizeManagedAccountRange(7, 0, 0, managedAccountDefaultTimezone)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ManagedAccountSnapshot{
		InstanceID: 8, SnapshotKind: model.ManagedAccountSnapshotKindInventory, RangeKey: managedAccountInventoryRangeKey,
		Timezone: managedAccountDefaultTimezone, SchemaVersion: 2, ObservedAt: 1234, Payload: `{"total":2}`,
		LastAttemptAt: 1234, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)
	require.NoError(t, saveManagedAccountSnapshot("", "", 8, model.ManagedAccountSnapshotKindInventory, accountRange, nil, errors.New("remote failed")))

	snapshot, err := findManagedAccountSnapshot(8, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	require.NoError(t, err)
	require.Equal(t, 2, snapshot.SchemaVersion)
	require.JSONEq(t, `{"total":2}`, snapshot.Payload)
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

func TestManagedAccountCustomRangeOnlyRecommendsInitialCollection(t *testing.T) {
	truncate(t)
	instance := &model.ManagedInstance{Name: "custom-range", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://custom.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	accountRange, err := NormalizeManagedAccountRange(0, 1_786_032_000, 1_786_118_399, managedAccountDefaultTimezone)
	require.NoError(t, err)

	missing, err := GetManagedAccountSnapshot(instance.Id, accountRange)
	require.NoError(t, err)
	require.True(t, missing.RefreshRecommended)

	require.NoError(t, model.DB.Create(&model.ManagedAccountSnapshot{
		InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindOutput, RangeKey: accountRange.RangeKey,
		WindowStart: accountRange.Start, WindowEnd: accountRange.End, Timezone: managedAccountDefaultTimezone,
		ObservedAt: 100, Payload: `{}`, LastAttemptAt: 100,
		LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)
	collected, err := GetManagedAccountSnapshot(instance.Id, accountRange)
	require.NoError(t, err)
	require.False(t, collected.RefreshRecommended)
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
	snapshot, err := findManagedAccountSnapshot(instance.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	require.NoError(t, err)
	require.Equal(t, 1, snapshot.SchemaVersion)
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

func TestEnqueueManagedAccountExportAcceptsOriginalAndLegacyRoundedAccountIDs(t *testing.T) {
	for _, requestedID := range []string{
		"8faa3804-86ab-4f4c-a090-e5111a406c74",
		"6822196335042536000",
	} {
		t.Run(requestedID, func(t *testing.T) {
			truncate(t)
			instance := &model.ManagedInstance{Name: "large-id-export", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://large-id.example.com"}
			require.NoError(t, model.DB.Create(instance).Error)
			const internalID int64 = 6822196335042536377
			payload, err := json.Marshal(managedinstance.InventoryPage{
				ResourceKind: "account", Total: 1,
				Items: []managedinstance.InventoryItem{{
					ID: internalID, IDText: "8faa3804-86ab-4f4c-a090-e5111a406c74", Name: "selected",
				}},
			})
			require.NoError(t, err)
			require.NoError(t, model.DB.Create(&model.ManagedAccountSnapshot{
				InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
				RangeKey: managedAccountInventoryRangeKey, Timezone: managedAccountDefaultTimezone,
				ObservedAt: 100, Payload: string(payload), LastAttemptAt: 100,
				LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
			}).Error)

			task, err := EnqueueManagedAccountExport(7, ManagedAccountExportRequest{
				Source: "inventory", Locale: "zh-CN",
				Window: managedinstance.TimeWindow{Start: 1786032000, End: 1786723199, Timezone: "Asia/Shanghai"},
				Items:  []ManagedAccountExportItemInput{{InstanceID: instance.Id, AccountID: requestedID}},
			})
			require.NoError(t, err)
			items, err := model.ListManagedExportItems(task.TaskID)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Equal(t, internalID, items[0].ResourceID)
		})
	}
}

func TestManagedAccountStandardSyncDueRequiresEveryPreset(t *testing.T) {
	instance := &model.ManagedInstance{Id: 9, Kind: model.ManagedInstanceKindSub2API}
	now := int64(10_000)
	latest := map[string]managedAccountScheduleState{
		managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey): {
			AttemptedAt: now, Status: model.ManagedInstanceCollectionSucceeded,
		},
	}
	require.True(t, managedAccountStandardSyncDue(instance, latest, now))
	for _, days := range managedAccountPresetDays {
		latest[managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindOutput, "preset-"+strconv.Itoa(days))] = managedAccountScheduleState{
			AttemptedAt: now, Status: model.ManagedInstanceCollectionSucceeded,
		}
	}
	require.False(t, managedAccountStandardSyncDue(instance, latest, now))
	require.False(t, managedAccountStandardSyncDue(instance, latest, now+int64(managedAccountSyncInterval/time.Second)-1))
	require.True(t, managedAccountStandardSyncDue(instance, latest, now+int64(managedAccountSyncInterval/time.Second)))

	conductor := &model.ManagedInstance{Id: 10, Kind: model.ManagedInstanceKindConductor}
	conductorLatest := map[string]managedAccountScheduleState{
		managedAccountScheduleKey(conductor.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey): {
			AttemptedAt: now, Status: model.ManagedInstanceCollectionSucceeded,
		},
	}
	require.True(t, managedAccountStandardSyncDue(conductor, conductorLatest, now))
	for _, days := range managedAccountPresetDays {
		conductorLatest[managedAccountScheduleKey(conductor.Id, model.ManagedAccountSnapshotKindOutput, "preset-"+strconv.Itoa(days))] = managedAccountScheduleState{
			AttemptedAt: now, Status: model.ManagedInstanceCollectionSucceeded,
		}
	}
	require.False(t, managedAccountStandardSyncDue(conductor, conductorLatest, now))

	failedKey := managedAccountScheduleKey(conductor.Id, model.ManagedAccountSnapshotKindOutput, "preset-1")
	conductorLatest[failedKey] = managedAccountScheduleState{
		AttemptedAt: now, Status: model.ManagedInstanceCollectionFailed,
	}
	require.False(t, managedAccountStandardSyncDue(conductor, conductorLatest, now+int64(managedAccountFailureCooldown/time.Second)-1))
	require.True(t, managedAccountStandardSyncDue(conductor, conductorLatest, now+int64(managedAccountFailureCooldown/time.Second)))
}

func TestManagedAccountDailyArchiveForcesMidnightSyncWithFailureCooldown(t *testing.T) {
	instance := &model.ManagedInstance{Id: 12, Kind: model.ManagedInstanceKindClaudeGateway}
	location := time.FixedZone(managedAccountDefaultTimezone, 8*60*60)
	boundary := time.Date(2026, time.September, 3, 0, 0, 0, 0, location).Unix()
	latest := make(map[string]managedAccountScheduleState)
	dailyArchived := make(map[string]bool)
	keys := []string{managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)}
	for _, days := range managedAccountPresetDays {
		keys = append(keys, managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindOutput, "preset-"+strconv.Itoa(days)))
	}
	for _, key := range keys {
		latest[key] = managedAccountScheduleState{AttemptedAt: boundary - 1, Status: model.ManagedInstanceCollectionSucceeded}
	}
	require.True(t, managedAccountDailyArchiveSyncDue(instance, latest, dailyArchived, boundary, boundary))

	for _, key := range keys {
		dailyArchived[key] = true
	}
	require.False(t, managedAccountDailyArchiveSyncDue(instance, latest, dailyArchived, boundary, boundary))

	missingKey := keys[len(keys)-1]
	dailyArchived[missingKey] = false
	latest[missingKey] = managedAccountScheduleState{AttemptedAt: boundary + 10, Status: model.ManagedInstanceCollectionFailed}
	require.False(t, managedAccountDailyArchiveSyncDue(instance, latest, dailyArchived, boundary, boundary+10+int64(managedAccountFailureCooldown/time.Second)-1))
	require.True(t, managedAccountDailyArchiveSyncDue(instance, latest, dailyArchived, boundary, boundary+10+int64(managedAccountFailureCooldown/time.Second)))
}

func TestClaudeGatewayStandardSyncDueBackfillsVendorMetadata(t *testing.T) {
	instance := &model.ManagedInstance{Id: 11, Kind: model.ManagedInstanceKindClaudeGateway}
	now := int64(10_000)
	latest := make(map[string]managedAccountScheduleState)
	for _, item := range append([]struct {
		kind string
		key  string
	}{{model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey}}, func() []struct {
		kind string
		key  string
	} {
		items := make([]struct {
			kind string
			key  string
		}, 0, len(managedAccountPresetDays))
		for _, days := range managedAccountPresetDays {
			items = append(items, struct {
				kind string
				key  string
			}{model.ManagedAccountSnapshotKindOutput, "preset-" + strconv.Itoa(days)})
		}
		return items
	}()...) {
		latest[managedAccountScheduleKey(instance.Id, item.kind, item.key)] = managedAccountScheduleState{
			AttemptedAt: now, Status: model.ManagedInstanceCollectionSucceeded,
			VendorMetadataAvailable: item.kind != model.ManagedAccountSnapshotKindInventory,
		}
	}
	require.True(t, managedAccountStandardSyncDue(instance, latest, now))

	inventoryKey := managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	state := latest[inventoryKey]
	state.VendorMetadataAvailable = true
	latest[inventoryKey] = state
	require.False(t, managedAccountStandardSyncDue(instance, latest, now))
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
