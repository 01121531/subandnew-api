package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	loggerINFO  = "INFO"
	loggerWarn  = "WARN"
	loggerError = "ERR"
	loggerDebug = "DEBUG"
	maxLogCount = 1000000
)

var (
	logCount         int
	setupLogLock     sync.Mutex
	setupLogWorking  bool
	currentLogPath   string
	currentLogPathMu sync.RWMutex
	currentLogFile   *os.File
)

func GetCurrentLogPath() string {
	currentLogPathMu.RLock()
	defer currentLogPathMu.RUnlock()
	return currentLogPath
}

func SetupLogger() {
	defer func() { setupLogWorking = false }()
	if *common.LogDir == "" || !setupLogLock.TryLock() {
		return
	}
	defer setupLogLock.Unlock()

	logPath := filepath.Join(*common.LogDir, fmt.Sprintf("huichuan-control-%s.log", time.Now().Format("20060102150405")))
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("failed to open log file: %v", err)
		return
	}
	currentLogPathMu.Lock()
	oldFile := currentLogFile
	currentLogPath = logPath
	currentLogFile = file
	currentLogPathMu.Unlock()

	common.LogWriterMu.Lock()
	gin.DefaultWriter = io.MultiWriter(os.Stdout, file)
	gin.DefaultErrorWriter = io.MultiWriter(os.Stderr, file)
	if oldFile != nil {
		_ = oldFile.Close()
	}
	common.LogWriterMu.Unlock()
}

func LogInfo(ctx context.Context, msg string)  { logHelper(ctx, loggerINFO, msg) }
func LogWarn(ctx context.Context, msg string)  { logHelper(ctx, loggerWarn, msg) }
func LogError(ctx context.Context, msg string) { logHelper(ctx, loggerError, msg) }

func LogDebug(ctx context.Context, msg string, args ...any) {
	if !common.DebugEnabled {
		return
	}
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	logHelper(ctx, loggerDebug, msg)
}

func logHelper(ctx context.Context, level, msg string) {
	id := any("SYSTEM")
	if ctx != nil {
		if requestID := ctx.Value(common.RequestIdKey); requestID != nil {
			id = requestID
		}
	}
	common.LogWriterMu.RLock()
	writer := gin.DefaultErrorWriter
	if level == loggerINFO {
		writer = gin.DefaultWriter
	}
	_, _ = fmt.Fprintf(writer, "[%s] %s | %v | %s\n", level, time.Now().Format("2006/01/02 - 15:04:05"), id, msg)
	common.LogWriterMu.RUnlock()
	logCount++
	if logCount > maxLogCount && !setupLogWorking {
		logCount = 0
		setupLogWorking = true
		gopool.Go(SetupLogger)
	}
}

func LogJson(ctx context.Context, msg string, obj any) {
	if !common.DebugEnabled {
		return
	}
	data, err := common.Marshal(obj)
	if err != nil {
		LogError(ctx, "json marshal failed: "+err.Error())
		return
	}
	LogDebug(ctx, "%s | %s", msg, data)
}
