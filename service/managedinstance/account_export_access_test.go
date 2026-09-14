package managedinstance

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestAccountWorkbookRemovesUnauthorizedColumnsAndFallbacks(t *testing.T) {
	t.Setenv("MANAGED_USAGE_EXPORT_DIR", t.TempDir())
	zero := 0.0
	row := AccountExportRow{Selection: AccountExportSelection{InstanceID: 1, InstanceName: "visible", InstanceKind: "claude_gateway", Account: InventoryItem{ID: 1, IDText: "uuid", Email: "private@email", VendorName: "private-vendor", Note: "private-note", Group: "private-group"}}, Amount: &zero, Status: "succeeded"}
	_, err := writeAccountExportWorkbook("systask_restricted_xlsx", AccountExportInput{Window: TimeWindow{Timezone: "Asia/Shanghai"}, VisibleFields: map[string]bool{"amount": true}}, []AccountExportRow{row}, 0)
	require.NoError(t, err)
	path, _, err := accountExportTaskPaths("systask_restricted_xlsx")
	require.NoError(t, err)
	book, err := excelize.OpenFile(path, excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	defer book.Close()
	rows, err := book.GetRows("账号导出", excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	require.Contains(t, rows[0], "消费金额 ($)")
	require.NotContains(t, rows[0], "供应商")
	require.NotContains(t, rows[0], "供应商邮箱")
	require.NotContains(t, rows[0], "录入时间")
	for _, cells := range rows {
		for _, cell := range cells {
			require.NotContains(t, cell, "private-")
			require.NotContains(t, cell, "private@email")
		}
	}
	for index, header := range rows[0] {
		if header == "消费金额 ($)" {
			require.Equal(t, "0", rows[1][index])
		}
	}
}

func TestUsageCSVPermissionsCoverDerivedValuesAndNestedFallbacks(t *testing.T) {
	a := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	require.False(t, usageCSVFieldAllowed(a, derivedField("account_billed_cost")))
	require.False(t, usageCSVFieldAllowed(a, field("user.email", "user_id")))
	require.False(t, usageCSVFieldAllowed(a, field("group.name", "group_id")))
	require.False(t, usageCSVFieldAllowed(a, field("content")))
	require.True(t, usageCSVFieldAllowed(a, field("account.name", "account_id")))
	a.Policy.Fields["amount"] = true
	require.True(t, usageCSVFieldAllowed(a, derivedField("account_billed_cost")))
}

func TestAccountWorkbookVendorEmailRequiresBothFields(t *testing.T) {
	t.Setenv("MANAGED_USAGE_EXPORT_DIR", t.TempDir())
	row := AccountExportRow{Selection: AccountExportSelection{InstanceID: 1, Account: InventoryItem{ID: 1, VendorName: "visible-vendor", VendorEmail: "hidden@example.test"}}, Status: "succeeded"}
	_, err := writeAccountExportWorkbook("systask_vendor_only", AccountExportInput{VisibleFields: map[string]bool{"vendor": true}}, []AccountExportRow{row}, 0)
	require.NoError(t, err)
	path, _, err := accountExportTaskPaths("systask_vendor_only")
	require.NoError(t, err)
	book, err := excelize.OpenFile(path)
	require.NoError(t, err)
	defer book.Close()
	rows, err := book.GetRows("账号导出")
	require.NoError(t, err)
	require.Contains(t, rows[0], "供应商")
	require.NotContains(t, rows[0], "供应商邮箱")
	for _, cells := range rows {
		require.NotContains(t, cells, "hidden@example.test")
	}
}
