package performance_setting

import (
	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/setting/config"
)

type PerformanceSetting struct {
	MonitorEnabled         bool `json:"monitor_enabled"`
	MonitorCPUThreshold    int  `json:"monitor_cpu_threshold"`
	MonitorMemoryThreshold int  `json:"monitor_memory_threshold"`
	MonitorDiskThreshold   int  `json:"monitor_disk_threshold"`
}

var performanceSetting = PerformanceSetting{
	MonitorEnabled:         true,
	MonitorCPUThreshold:    90,
	MonitorMemoryThreshold: 90,
	MonitorDiskThreshold:   95,
}

func init() {
	config.GlobalConfig.Register("performance_setting", &performanceSetting)
	syncToCommon()
}

func syncToCommon() {
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{
		Enabled:         performanceSetting.MonitorEnabled,
		CPUThreshold:    performanceSetting.MonitorCPUThreshold,
		MemoryThreshold: performanceSetting.MonitorMemoryThreshold,
		DiskThreshold:   performanceSetting.MonitorDiskThreshold,
	})
}

func UpdateAndSync() {
	syncToCommon()
}
