package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/i18n"
	"github.com/01121531/subandnew-api/internal/legacydata"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/middleware"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/oauth"
	"github.com/01121531/subandnew-api/pkg/systemupdate"
	"github.com/01121531/subandnew-api/router"
	"github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/billingalert"
	_ "github.com/01121531/subandnew-api/setting/performance_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	_ "net/http/pprof"
)

//go:embed web/default/dist
var buildFS embed.FS

//go:embed web/default/dist/index.html
var indexPage []byte

func main() {
	if legacydata.IsCommand(os.Args[1:]) {
		_ = godotenv.Load(".env")
		common.InitEnv()
		os.Exit(legacydata.Run(os.Args[1:], os.Stdout, os.Stderr))
	}
	if systemupdate.RunHelperIfRequested() {
		return
	}
	startTime := time.Now()

	err := InitResources()
	if err != nil {
		common.FatalLog("failed to initialize resources: " + err.Error())
		return
	}
	systemupdate.RecoverInterruptedUpdate(common.Version)
	if common.IsMasterNode {
		repair, repairErr := billingalert.RepairLegacyDiscountRates()
		if repairErr != nil {
			common.FatalLog("failed to repair billing alert discount rates: " + repairErr.Error())
			return
		}
		if repair.CorrectedTotal() > 0 {
			common.SysLog(fmt.Sprintf(
				"billing alert discount repair completed: rules=%d cycles=%d evaluations=%d events=%d",
				repair.Rules, repair.Cycles, repair.Evaluations, repair.Events,
			))
		}
		if repair.Invalid > 0 {
			common.SysError(fmt.Sprintf("billing alert discount repair found %d invalid values", repair.Invalid))
		}
	}

	common.SysLog("SubAndNew API " + common.Version + " started")
	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	if common.DebugEnabled {
		common.SysLog("running in debug mode")
	}

	// 热更新配置
	go model.SyncOptions(common.SyncFrequency)

	// 周期性重载授权策略，保证多节点/多 master 部署下权限变更能传播到每个实例
	go authz.StartPolicySync(common.SyncFrequency)

	// Report this process as a system instance so the System Info page can show
	// all currently alive nodes in multi-instance deployments.
	service.StartSystemInstanceReporter()

	// Run only handlers registered by the control-plane task packages.
	service.StartSystemTaskRunner()
	service.StartManagedDashboardCollector()
	service.StartManagedConductorRealtimeCollector()
	service.StartManagedSub2RealtimeCollector()

	if os.Getenv("ENABLE_PPROF") == "true" {
		gopool.Go(func() {
			log.Println(http.ListenAndServe("0.0.0.0:8005", nil))
		})
		go common.Monitor()
		common.SysLog("pprof enabled")
	}

	err = common.StartPyroScope()
	if err != nil {
		common.SysError(fmt.Sprintf("start pyroscope error : %v", err))
	}

	// Initialize HTTP server
	server := gin.New()
	server.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		common.SysLog(fmt.Sprintf("panic detected: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Panic detected, error: %v. Please report it here: https://github.com/01121531/subandnew-api/issues", err),
				"type":    "subandnew_panic",
			},
		})
	}))
	server.Use(middleware.RequestId())
	server.Use(middleware.Version())
	server.Use(middleware.I18n())
	middleware.SetUpLogger(server)
	// Initialize session store
	store := cookie.NewStore([]byte(common.SessionSecret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   2592000, // 30 days
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	server.Use(sessions.Sessions("session", store))

	// 设置路由
	router.SetRouter(server, router.ThemeAssets{
		BuildFS:   buildFS,
		IndexPage: indexPage,
	})
	var port = os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(*common.Port)
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: server,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			common.FatalLog("failed to start HTTP server: " + err.Error())
		}
	}()

	time.Sleep(100 * time.Millisecond)

	common.LogStartupSuccess(startTime, port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	shutdownTimeoutSeconds := common.GetEnvOrDefault("SHUTDOWN_TIMEOUT_SECONDS", 120)
	select {
	case sig := <-quit:
		common.SysLog(fmt.Sprintf("received signal: %v, shutting down...", sig))
	case reason := <-systemupdate.ShutdownRequests():
		shutdownTimeoutSeconds = systemupdate.ShutdownTimeoutSeconds()
		common.SysLog("received internal shutdown request for " + reason)
	}
	signal.Stop(quit)

	// SSE streams may run for minutes; give them time to finish before forced exit
	shutdownTimeout := time.Duration(shutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		common.SysError(fmt.Sprintf("server forced to shutdown: %v", err))
		// Shutdown returning on deadline does not wait for active handlers. Close
		// their connections, but keep shared database and snapshot resources open
		// until process exit so remaining goroutines cannot observe
		// "sql: database is closed" during forced upgrade termination.
		if closeErr := srv.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			common.SysError(fmt.Sprintf("failed to close active server connections: %v", closeErr))
		}
	}
	if err := service.StopManagedConductorRealtimeCollector(ctx); err != nil {
		common.SysError("managed conductor realtime collector did not stop before shutdown deadline: " + err.Error())
	}
	if err := service.StopSystemTaskRunner(ctx); err != nil {
		common.SysError("system task runner did not stop before shutdown deadline: " + err.Error())
	}
	if err := service.StopSystemInstanceReporter(ctx); err != nil {
		common.SysError("system instance reporter did not stop before shutdown deadline: " + err.Error())
	}
	common.SysLog("server exited")
}

func InitResources() error {
	// Initialize resources here if needed
	// This is a placeholder function for future resource initialization
	err := godotenv.Load(".env")
	if err != nil {
		if common.DebugEnabled {
			common.SysLog("No .env file found, using default environment variables. If needed, please create a .env file and set the relevant variables.")
		}
	}

	// 加载环境变量
	common.InitEnv()

	logger.SetupLogger()

	// Initialize the shared outbound client used by control-plane integrations.

	// Initialize SQL Database
	err = model.InitDB()
	if err != nil {
		common.FatalLog("failed to initialize database: " + err.Error())
		return err
	}
	if err = authz.Init(model.DB); err != nil {
		common.FatalLog("failed to initialize authorization: " + err.Error())
		return err
	}

	model.CheckSetup()

	// Initialize options, should after model.InitDB()
	model.InitOptionMap()
	// Initialize Redis
	err = common.InitRedisClient()
	if err != nil {
		return err
	}

	// 启动系统监控
	common.StartSystemMonitor()

	// Initialize i18n
	err = i18n.Init()
	if err != nil {
		common.SysError("failed to initialize i18n: " + err.Error())
		// Don't return error, i18n is not critical
	} else {
		common.SysLog("i18n initialized with languages: " + strings.Join(i18n.SupportedLanguages(), ", "))
	}
	// Register user language loader for lazy loading
	i18n.SetUserLangLoader(model.GetUserLanguage)

	// Load custom OAuth providers from database
	err = oauth.LoadCustomProviders()
	if err != nil {
		common.SysError("failed to load custom OAuth providers: " + err.Error())
		// Don't return error, custom OAuth is not critical
	}

	return nil
}
