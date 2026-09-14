package billingalert

import (
	"encoding/json"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
)

func restrictedAlertAccess(ids ...int64) *authz.DataAccess {
	p := model.EmptyAdminDataPolicy()
	p.InstanceIDs = ids
	return &authz.DataAccess{Role: common.RoleAdminUser, Policy: p}
}

func TestAlertRecordsScopeBeforePaginationAndDenyExplicitInstance(t *testing.T) {
	setupRepositoryTestDB(t)
	for _, event := range []*model.BillingAlertEvent{
		{EventKey: "allowed-old", InstanceID: 1, CreatedAt: 1},
		{EventKey: "hidden-new", InstanceID: 2, CreatedAt: 3},
		{EventKey: "aggregate", InstanceID: 0, CreatedAt: 4},
		{EventKey: "allowed-new", InstanceID: 1, CreatedAt: 2},
	} {
		require.NoError(t, model.DB.Create(event).Error)
	}
	a := restrictedAlertAccess(1)
	page, err := ListAlertRecords(AlertRecordFilter{Page: 1, PageSize: 1}, a)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "allowed-new", page.Items[0].EventKey)
	page, err = ListAlertRecords(AlertRecordFilter{Page: 2, PageSize: 1}, a)
	require.NoError(t, err)
	require.Equal(t, "allowed-old", page.Items[0].EventKey)
	page, err = ListAlertRecords(AlertRecordFilter{}, restrictedAlertAccess())
	require.NoError(t, err)
	require.Zero(t, page.Total)
	require.Empty(t, page.Items)
	_, err = ListAlertRecords(AlertRecordFilter{InstanceID: 2}, a)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
	var hidden model.BillingAlertEvent
	require.NoError(t, model.DB.Where("event_key = ?", "aggregate").First(&hidden).Error)
	_, err = GetAlertRecord(hidden.ID, a)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
}

func TestAlertRuleScopeExcludesMixedAndUnboundRules(t *testing.T) {
	setupRepositoryTestDB(t)
	for id := int64(1); id <= 3; id++ {
		require.NoError(t, model.DB.Create(&model.BillingAlertRule{ID: id, Name: string(rune('a' + id))}).Error)
	}
	require.NoError(t, model.DB.Create(&[]model.BillingAlertRuleInstance{
		{RuleID: 1, InstanceID: 1}, {RuleID: 2, InstanceID: 1}, {RuleID: 2, InstanceID: 2},
	}).Error)
	a := restrictedAlertAccess(1)
	var ids []int64
	q := ScopeRules(model.DB.Model(&model.BillingAlertRule{}), a, "billing_alert_rules", "billing_alert_rule_instances")
	require.NoError(t, q.Pluck("id", &ids).Error)
	require.Equal(t, []int64{1}, ids)
	require.ErrorIs(t, CheckRuleAccess(a, 2, "billing_alert_rules", "billing_alert_rule_instances"), authz.ErrDataForbidden)
	require.ErrorIs(t, a.CheckInstances([]int64{1, 2}), authz.ErrDataForbidden)
	require.ErrorIs(t, restrictedAlertAccess().CheckInstances([]int64{1}), authz.ErrDataForbidden)
}

func TestAlertProjectionUsesMetricAndClosesOpaqueFields(t *testing.T) {
	a := restrictedAlertAccess(1)
	a.Policy.Fields["rpm"] = true
	a.Policy.Fields["time"] = true
	input := map[string]any{
		"metric": []any{map[string]any{
			"instance_ids": []int64{1},
			"conditions": []any{
				map[string]any{"metric": "rpm", "threshold": "21", "current_value": "22"},
				map[string]any{"metric": "today_cost", "threshold": "secret-cost", "current_value": "secret-value"},
				map[string]any{"metric": "unknown", "threshold": "secret-unknown"},
			},
			"last_values": "{secret-json}", "message": "secret-mail-body", "email_error": "secret-smtp",
			"email_recipients": "secret-recipient", "email_sent_at": 987654,
			"created_at": 123, "future_field": "secret-future",
		}},
	}
	projected, err := ProjectAlertData(input, a)
	require.NoError(t, err)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
	require.NotContains(t, string(encoded), "987654")
	require.Contains(t, string(encoded), `"threshold":"21"`)
	require.Contains(t, string(encoded), `"current_value":"22"`)
	require.Contains(t, string(encoded), `"created_at":123`)
	require.Contains(t, string(encoded), `"metric":[`)
	billing, err := ProjectAlertData(map[string]any{"source_type": "billing", "threshold": "hidden", "usd_total": "hidden", "threshold_name": "hidden"}, a)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"source_type": "billing"}, billing)
}

func TestAlertProjectionScopesSourcesEvenWithAllFields(t *testing.T) {
	a := restrictedAlertAccess(1)
	a.Policy.Fields = model.FullAdminDataPolicy().Fields
	value, err := ProjectAlertData(map[string]any{
		"sources": []any{
			map[string]any{"instance_id": 1, "instance_name": "visible"},
			map[string]any{"instance_id": 2, "instance_name": "hidden"},
			map[string]any{"source_instance_id": 2, "instance_name": "hidden-source"},
		},
		"states": []any{
			map[string]any{"instance_id": 0, "last_values": "hidden-aggregate"},
			map[string]any{"instance_id": 2, "last_values": "hidden-history"},
		},
	}, a)
	require.NoError(t, err)
	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.Contains(t, string(data), "visible")
	require.NotContains(t, string(data), "hidden")
}

func TestAlertFiltersRejectHiddenFields(t *testing.T) {
	a := restrictedAlertAccess(1)
	for _, filter := range []AlertRecordFilter{
		{Currency: "USD"}, {Recipient: "ops"}, {StartTime: 1}, {EventType: "threshold"},
		{MetricKey: "today_cost"}, {RuleID: 1},
	} {
		require.ErrorIs(t, ValidateRecordFilter(filter, a), authz.ErrDataForbidden)
	}
	setupRepositoryTestDB(t)
	for _, filter := range []InstanceAlertFilter{{Status: "open"}, {DeliveryStatus: "sent"}, {StartTime: 1}, {Search: "ops"}} {
		_, err := ListInstanceAlerts(filter, a)
		require.ErrorIs(t, err, authz.ErrDataForbidden)
	}
}

func TestInstanceAlertDeliveriesCannotFollowAnotherInstanceSource(t *testing.T) {
	setupRepositoryTestDB(t)
	alert := &model.ManagedInstanceAlert{InstanceId: 2, EmailRecipients: "hidden@example.com", EmailStatus: "sent"}
	require.NoError(t, model.DB.Create(alert).Error)
	event := &model.BillingAlertEvent{EventKey: "wrong-source", SourceType: model.AlertSourceInstance,
		InstanceID: 1, SourceRecordID: alert.Id}
	require.NoError(t, model.DB.Create(event).Error)
	view, err := GetAlertRecord(event.ID, restrictedAlertAccess(1))
	require.NoError(t, err)
	require.Empty(t, view.Deliveries)
}
