package service

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/logger"
	"github.com/01121531/HUICHUAN-AI/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const systemInstanceReportInterval = 30 * time.Second

var (
	systemInstanceReporterOnce sync.Once
	systemInstanceReporterMu   sync.Mutex
	systemInstanceReporterStop context.CancelFunc
	systemInstanceReporterWG   sync.WaitGroup
)

type SystemInstanceInfo struct {
	SchemaVersion int                       `json:"schema_version"`
	Node          common.NodeIdentity       `json:"node"`
	Role          SystemInstanceRoleInfo    `json:"role"`
	Runtime       SystemInstanceRuntimeInfo `json:"runtime"`
	Host          SystemInstanceHostInfo    `json:"host"`
	Resources     SystemInstanceResources   `json:"resources,omitempty"`
	Extra         map[string]any            `json:"extra,omitempty"`
}

type SystemInstanceRoleInfo struct {
	IsMaster bool `json:"is_master"`
}

type SystemInstanceRuntimeInfo struct {
	Version   string `json:"version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	StartedAt int64  `json:"started_at"`
}

type SystemInstanceHostInfo struct {
	Hostname string `json:"hostname"`
}

type SystemInstanceResources struct {
	CPU     SystemInstanceResourceUsage  `json:"cpu"`
	Memory  SystemInstanceResourceUsage  `json:"memory"`
	Storage SystemInstanceStorageMetrics `json:"storage"`
}

type SystemInstanceResourceUsage struct {
	UsagePercent float64 `json:"usage_percent"`
}

type SystemInstanceStorageMetrics struct {
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

func StartSystemInstanceReporter() {
	systemInstanceReporterOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		systemInstanceReporterMu.Lock()
		systemInstanceReporterStop = cancel
		systemInstanceReporterMu.Unlock()
		systemInstanceReporterWG.Add(1)
		gopool.Go(func() {
			defer systemInstanceReporterWG.Done()
			reportSystemInstanceWithLog()

			ticker := time.NewTicker(systemInstanceReportInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					reportSystemInstanceWithLog()
				}
			}
		})
	})
}

func StopSystemInstanceReporter(ctx context.Context) error {
	systemInstanceReporterMu.Lock()
	cancel := systemInstanceReporterStop
	systemInstanceReporterMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	done := make(chan struct{})
	go func() {
		systemInstanceReporterWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ReportCurrentSystemInstance() error {
	identity := common.GetNodeIdentity()
	hostname, hostnameErr := os.Hostname()
	if strings.TrimSpace(identity.Name) == "" {
		if hostnameErr != nil || strings.TrimSpace(hostname) == "" {
			return fmt.Errorf("system instance node name is empty")
		}
		identity.Name = hostname
		identity.Source = common.NodeNameSourceHostname
		identity.ManuallyConfigured = false
		identity.ShouldConfigureManually = true
	}
	systemStatus := common.GetSystemStatus()
	diskInfo := common.GetDiskSpaceInfo()
	info := SystemInstanceInfo{
		SchemaVersion: 1,
		Node:          identity,
		Role: SystemInstanceRoleInfo{
			IsMaster: common.IsMasterNode,
		},
		Runtime: SystemInstanceRuntimeInfo{
			Version:   common.Version,
			GOOS:      runtime.GOOS,
			GOARCH:    runtime.GOARCH,
			StartedAt: common.StartTime,
		},
		Host: SystemInstanceHostInfo{
			Hostname: hostname,
		},
		Resources: SystemInstanceResources{
			CPU: SystemInstanceResourceUsage{
				UsagePercent: systemStatus.CPUUsage,
			},
			Memory: SystemInstanceResourceUsage{
				UsagePercent: systemStatus.MemoryUsage,
			},
			Storage: SystemInstanceStorageMetrics{
				TotalBytes:  diskInfo.Total,
				UsedBytes:   diskInfo.Used,
				FreeBytes:   diskInfo.Free,
				UsedPercent: diskInfo.UsedPercent,
			},
		},
	}
	return model.UpsertSystemInstance(identity.Name, info, common.StartTime, common.GetTimestamp())
}

func reportSystemInstanceWithLog() {
	if err := ReportCurrentSystemInstance(); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system instance report failed: %v", err))
	}
}
