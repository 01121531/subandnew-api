package controller

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/logger"
	"github.com/gin-gonic/gin"
)

type PerformanceStats struct {
	MemoryStats MemoryStats                     `json:"memory_stats"`
	DiskSpace   common.DiskSpaceInfo            `json:"disk_space_info"`
	Monitor     common.PerformanceMonitorConfig `json:"monitor"`
	InContainer bool                            `json:"is_running_in_container"`
}

type MemoryStats struct {
	Alloc        uint64 `json:"alloc"`
	TotalAlloc   uint64 `json:"total_alloc"`
	Sys          uint64 `json:"sys"`
	NumGC        uint32 `json:"num_gc"`
	NumGoroutine int    `json:"num_goroutine"`
}

func GetPerformanceStats(c *gin.Context) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	common.ApiSuccess(c, PerformanceStats{
		MemoryStats: MemoryStats{
			Alloc: memory.Alloc, TotalAlloc: memory.TotalAlloc, Sys: memory.Sys,
			NumGC: memory.NumGC, NumGoroutine: runtime.NumGoroutine(),
		},
		DiskSpace:   common.GetDiskSpaceInfo(),
		Monitor:     common.GetPerformanceMonitorConfig(),
		InContainer: common.IsRunningInContainer(),
	})
}

func ForceGC(c *gin.Context) {
	runtime.GC()
	common.ApiSuccess(c, nil)
}

type LogFileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type LogFilesResponse struct {
	LogDir     string        `json:"log_dir"`
	Enabled    bool          `json:"enabled"`
	FileCount  int           `json:"file_count"`
	TotalSize  int64         `json:"total_size"`
	OldestTime *time.Time    `json:"oldest_time,omitempty"`
	NewestTime *time.Time    `json:"newest_time,omitempty"`
	Files      []LogFileInfo `json:"files"`
}

func getLogFiles() ([]LogFileInfo, error) {
	if *common.LogDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(*common.LogDir)
	if err != nil {
		return nil, err
	}
	files := make([]LogFileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil {
			files = append(files, LogFileInfo{Name: entry.Name(), Size: info.Size(), ModTime: info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.After(files[j].ModTime) })
	return files, nil
}

func GetLogFiles(c *gin.Context) {
	if *common.LogDir == "" {
		common.ApiSuccess(c, LogFilesResponse{Enabled: false})
		return
	}
	files, err := getLogFiles()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response := LogFilesResponse{LogDir: *common.LogDir, Enabled: true, FileCount: len(files), Files: files}
	for index, file := range files {
		response.TotalSize += file.Size
		if index == 0 || file.ModTime.Before(*response.OldestTime) {
			value := file.ModTime
			response.OldestTime = &value
		}
		if index == 0 || file.ModTime.After(*response.NewestTime) {
			value := file.ModTime
			response.NewestTime = &value
		}
	}
	common.ApiSuccess(c, response)
}

func CleanupLogFiles(c *gin.Context) {
	mode := c.Query("mode")
	value, err := strconv.Atoi(c.Query("value"))
	if (mode != "by_count" && mode != "by_days") || err != nil || value < 1 {
		common.ApiErrorMsg(c, "invalid log cleanup parameters")
		return
	}
	files, err := getLogFiles()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	activePath := logger.GetCurrentLogPath()
	cutoff := time.Now().AddDate(0, 0, -value)
	deleted, freed := 0, int64(0)
	failed := make([]string, 0)
	for index, file := range files {
		shouldDelete := mode == "by_count" && index >= value || mode == "by_days" && file.ModTime.Before(cutoff)
		path := filepath.Join(*common.LogDir, file.Name)
		if !shouldDelete || path == activePath {
			continue
		}
		if removeErr := os.Remove(path); removeErr != nil {
			failed = append(failed, file.Name)
			continue
		}
		deleted++
		freed += file.Size
	}
	result := gin.H{"deleted_count": deleted, "freed_bytes": freed, "failed_files": failed}
	if len(failed) > 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("failed to delete %d log files", len(failed)), "data": result})
		return
	}
	common.ApiSuccess(c, result)
}
