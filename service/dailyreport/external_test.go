package dailyreport

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	controlplaneservice "github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestListClaudeGatewaySuppliersUsesInventorySnapshot(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ManagedInstance{}, &model.ManagedAccountSnapshot{}, &model.ManagedInstanceSnapshot{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	instance := model.ManagedInstance{Name: "gateway", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway.invalid"}
	require.NoError(t, db.Create(&instance).Error)
	page := managedinstance.InventoryPage{ResourceKind: "account", Total: 2, Items: []managedinstance.InventoryItem{
		{VendorID: "vendor-a", VendorName: "Vendor A"},
		{VendorID: "", VendorName: ""},
	}}
	payload, err := json.Marshal(page)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedAccountSnapshot{
		InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
		RangeKey: "inventory", Timezone: "Asia/Shanghai", ObservedAt: time.Now().Unix(),
		Payload: string(payload), LastAttemptAt: time.Now().Unix(),
		LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)
	items, err := ListClaudeGatewaySuppliers(t.Context(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, []SupplierOption{{Code: "vendor-a", Name: "Vendor A"}, {Code: "__platform__", Name: "平台自有"}}, items)
	require.NoError(t, db.Model(&model.ManagedAccountSnapshot{}).Where("instance_id = ?", instance.Id).Update("payload", `{"resource_kind":"account","total":1,"items":[{"vendor_id":"vendor-b","vendor_name":"Vendor B"}]}`).Error)
	cached, err := ListClaudeGatewaySuppliers(t.Context(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, items, cached)
	require.NoError(t, db.Model(&model.ManagedAccountSnapshot{}).Where("instance_id = ?", instance.Id).Update("observed_at", time.Now().Unix()+1).Error)
	refreshed, err := ListClaudeGatewaySuppliers(t.Context(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, []SupplierOption{{Code: "vendor-b", Name: "Vendor B"}}, refreshed)
	_, err = controlplaneservice.GetManagedAccountInventorySnapshot(instance.Id)
	require.NoError(t, err)
}
