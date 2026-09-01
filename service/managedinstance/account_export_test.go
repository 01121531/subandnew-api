package managedinstance

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"io"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestWriteAccountExportWorkbookPreservesRowsAndFormatting(t *testing.T) {
	t.Setenv("MANAGED_USAGE_EXPORT_DIR", t.TempDir())
	enabled := false
	requests, input, output := 15116.0, 47175232.0, 14599531.0
	cacheWrite, cacheRead, amount := 338279171.0, 478948958.0, 2960.3409
	total := input + output + cacheWrite + cacheRead
	rows := []AccountExportRow{{
		Selection: AccountExportSelection{
			InstanceID: 7, InstanceName: "gateway-a", InstanceKind: model.ManagedInstanceKindClaudeGateway,
			SourceName: "root-direct", Account: InventoryItem{
				ID: 6822196335042536000, IDText: "6822196335042536000", Name: "allen-0811-m01",
				Email: "account@example.com", VendorName: "供应商 A", VendorEmail: "vendor@example.com",
				Type: "Max", CreatedAt: 1786468440, Enabled: &enabled,
			},
		},
		Requests: &requests, InputTokens: &input, OutputTokens: &output,
		CacheWriteTokens: &cacheWrite, CacheReadTokens: &cacheRead, Amount: &amount,
		TotalTokens: &total, Status: model.ManagedInstanceCollectionSucceeded,
	}}
	artifact, err := writeAccountExportWorkbook("systask_xlsx_structure", AccountExportInput{
		Window: TimeWindow{Start: 1786032000, End: 1786723199, Timezone: "Asia/Shanghai"}, Locale: "zh-CN",
	}, rows, 0)
	require.NoError(t, err)
	require.Equal(t, 1, artifact.RecordCount)
	require.Equal(t, 0, artifact.WarningCount)

	path, _, err := accountExportTaskPaths("systask_xlsx_structure")
	require.NoError(t, err)
	workbook, err := excelize.OpenFile(path, excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = workbook.Close() })
	require.Equal(t, "账号归属", mustCell(t, workbook, "A1"))
	require.Equal(t, "供应商", mustCell(t, workbook, "B1"))
	require.Equal(t, "供应商邮箱", mustCell(t, workbook, "C1"))
	require.Equal(t, "消费金额 ($)", mustCell(t, workbook, "N1"))
	require.Equal(t, "实例", mustCell(t, workbook, "O1"))
	require.Equal(t, "平台", mustCell(t, workbook, "P1"))
	require.Equal(t, "统计状态", mustCell(t, workbook, "T1"))
	require.Equal(t, "统计错误", mustCell(t, workbook, "U1"))
	require.Equal(t, "供应商 A", mustCell(t, workbook, "B2"))
	require.Equal(t, "vendor@example.com", mustCell(t, workbook, "C2"))
	require.Equal(t, "account@example.com", mustCell(t, workbook, "D2"))
	require.Equal(t, "6822196335042536000", mustCell(t, workbook, "Q2"))
	require.Equal(t, "2960.3409", mustCell(t, workbook, "N2"))
	panes, err := workbook.GetPanes("账号导出")
	require.NoError(t, err)
	require.True(t, panes.Freeze)
	require.Equal(t, 1, panes.YSplit)
	require.NotZero(t, mustStyle(t, workbook, "A1"))
	requireWorksheetAutoFilter(t, path)
}

func TestCollectAccountExportRowsUsesClaudeGatewayOutputCounters(t *testing.T) {
	requests, tokens, amount := 1476.0, 66_759_000.0, 294.8689
	enabled := true
	rows, warnings, err := collectAccountExportRows(context.Background(), AccountExportInput{
		Source: "account_output",
		Window: TimeWindow{Start: 1787587200, End: 1787759999, Timezone: "Asia/Shanghai"},
		Selected: []AccountExportSelection{{
			InstanceID: 3, InstanceName: "gateway", InstanceKind: model.ManagedInstanceKindClaudeGateway,
			Account: InventoryItem{
				ID: 1, IDText: "1", Name: "leo-4", CreatedAt: 1787711520, Enabled: &enabled,
				Requests: &requests, Tokens: &tokens, Cost: &amount, CostUnit: "usd",
			},
		}},
	}, nil)
	require.NoError(t, err)
	require.Zero(t, warnings)
	require.Len(t, rows, 1)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, rows[0].Status)
	require.Equal(t, requests, *rows[0].Requests)
	require.Equal(t, tokens, *rows[0].TotalTokens)
	require.Equal(t, amount, *rows[0].Amount)
	require.Nil(t, rows[0].InputTokens)
	require.Nil(t, rows[0].OutputTokens)
	require.Nil(t, rows[0].CacheWriteTokens)
	require.Nil(t, rows[0].CacheReadTokens)
}

func mustCell(t *testing.T, workbook *excelize.File, cell string) string {
	t.Helper()
	value, err := workbook.GetCellValue("账号导出", cell, excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	return value
}

func mustStyle(t *testing.T, workbook *excelize.File, cell string) int {
	t.Helper()
	style, err := workbook.GetCellStyle("账号导出", cell)
	require.NoError(t, err)
	return style
}

func requireWorksheetAutoFilter(t *testing.T, path string) {
	t.Helper()
	archive, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer archive.Close()
	for _, file := range archive.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		reader, err := file.Open()
		require.NoError(t, err)
		defer reader.Close()
		decoder := xml.NewDecoder(reader)
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if start, ok := token.(xml.StartElement); ok && start.Name.Local == "autoFilter" {
				return
			}
		}
	}
	t.Fatal("workbook worksheet does not contain an auto filter")
}
