package managedinstance

import (
	"archive/zip"
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
				Email: "account@example.com", Type: "Max", CreatedAt: 1786468440, Enabled: &enabled,
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
	require.Equal(t, "消费金额 ($)", mustCell(t, workbook, "L1"))
	require.Equal(t, "统计错误", mustCell(t, workbook, "S1"))
	require.Equal(t, "account@example.com", mustCell(t, workbook, "B2"))
	require.Equal(t, "6822196335042536000", mustCell(t, workbook, "O2"))
	require.Equal(t, "2960.3409", mustCell(t, workbook, "L2"))
	panes, err := workbook.GetPanes("账号导出")
	require.NoError(t, err)
	require.True(t, panes.Freeze)
	require.Equal(t, 1, panes.YSplit)
	require.NotZero(t, mustStyle(t, workbook, "A1"))
	requireWorksheetAutoFilter(t, path)
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
