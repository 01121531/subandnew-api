package accountexport

import (
	"bufio"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/stretchr/testify/require"
)

func TestDeliveryPersistsSMTPOutcomesWithoutAutomaticReplay(t *testing.T) {
	for _, tc := range []struct{ reply, status string }{{"250 accepted\r\n", "sent"}, {"451 rejected\r\n", "retry"}, {"550 rejected\r\n", "failed"}, {"", "uncertain"}} {
		t.Run(tc.status, func(t *testing.T) {
			db, user, instance := setupSelectionTest(t, 1)
			require.NoError(t, db.AutoMigrate(&model.ManagedAccountExportSchedule{}, &model.ManagedAccountExportRun{}, &model.ManagedAccountExportDelivery{}, &model.ManagedUsageExport{}, &model.ManagedExportItem{}, &model.SMTPSetting{}))
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			raw := make(chan string, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					raw <- ""
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = io.WriteString(conn, "220 localhost ESMTP\r\n")
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						raw <- ""
						return
					}
					if strings.HasPrefix(line, "DATA") {
						_, _ = io.WriteString(conn, "354 continue\r\n")
						var body strings.Builder
						for {
							line, err = reader.ReadString('\n')
							if err != nil {
								raw <- ""
								return
							}
							if line == ".\r\n" {
								break
							}
							body.WriteString(line)
						}
						if tc.reply != "" {
							_, _ = io.WriteString(conn, tc.reply)
						}
						raw <- body.String()
						return
					}
					_, _ = io.WriteString(conn, "250 OK\r\n")
				}
			}()
			require.NoError(t, db.Create(&model.SMTPSetting{ID: 1, Enabled: true, Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Security: "none", FromAddress: "sender@example.test"}).Error)
			view, err := SavePlan(user.Id, 0, PlanInput{Name: "测试报表", Enabled: true, Config: Config{Scope: "dynamic", Schedule: Schedule{Kind: "daily", Hour: 9}, Period: ReportPeriod{Kind: "last30"}, Query: managedaccount.Query{InstanceIDs: []int64{instance.Id}, Dataset: "inventory"}, Recipients: []string{"one@example.test", "other@example.test"}}})
			require.NoError(t, err)
			run, err := ExecuteNow(t.Context(), user.Id, view.ID, view.Version)
			require.NoError(t, err)
			dir := t.TempDir()
			t.Setenv("MANAGED_USAGE_EXPORT_DIR", dir)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "managed-account-"+run.TaskID+".xlsx"), []byte("fixture workbook"), 0600))
			require.NoError(t, db.Model(&model.ManagedUsageExport{}).Where("task_id = ?", run.TaskID).Updates(map[string]any{"status": "succeeded", "file_name": "测试报表.xlsx", "file_size": 16, "expires_at": time.Now().Add(time.Hour).Unix()}).Error)
			record, err := model.GetManagedUsageExport(run.TaskID)
			require.NoError(t, err)
			var d model.ManagedAccountExportDelivery
			require.NoError(t, db.Where("run_id = ?", run.ID).Order("id").First(&d).Error)
			require.NoError(t, deliver(t.Context(), "test-worker", view.ManagedAccountExportSchedule, *run, record, d))
			body := <-raw
			require.Contains(t, body, "one@example.test")
			require.NotContains(t, body, "other@example.test")
			require.Contains(t, body, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			require.NoError(t, db.First(&d, d.ID).Error)
			require.Equal(t, tc.status, d.Status)
			require.Equal(t, 1, d.Attempts)
			if tc.status == "retry" {
				require.InDelta(t, time.Now().Add(time.Minute).Unix(), d.NextAttemptAt, 3)
			}
			if tc.status == "uncertain" {
				require.ErrorIs(t, RetryDelivery(user.Id, view.ID, d.ID, d.Version, false), ErrInvalidSchedule)
				require.NoError(t, RetryDelivery(user.Id, view.ID, d.ID, d.Version, true))
				require.NoError(t, db.First(&d, d.ID).Error)
				require.Equal(t, "pending", d.Status)
				var exports int64
				require.NoError(t, db.Model(&model.ManagedUsageExport{}).Count(&exports).Error)
				require.Equal(t, int64(1), exports)
			}
		})
	}
}
