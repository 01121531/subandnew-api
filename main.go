package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	httppprof "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
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
	assistantsecrets "github.com/01121531/subandnew-api/service/assistant/secrets"
	assistantworker "github.com/01121531/subandnew-api/service/assistant/worker"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/billingalert"
	"github.com/01121531/subandnew-api/service/managedinstance"
	_ "github.com/01121531/subandnew-api/setting/performance_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
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

	leadershipLost := make(chan struct{}, 1)
	var controlPlaneMu sync.Mutex
	controlPlaneShuttingDown := false
	var aiAssistantWorker *assistantworker.Worker
	startControlPlaneServices := func() {
		controlPlaneMu.Lock()
		defer controlPlaneMu.Unlock()
		if controlPlaneShuttingDown {
			return
		}
		if err := runControlPlaneRepairs(); err != nil {
			common.SysError("control plane repair failed: " + err.Error())
			select {
			case leadershipLost <- struct{}{}:
			default:
			}
			return
		}
		service.StartSystemTaskRunner()
		service.StartManagedDashboardCollector()
		service.StartManagedConductorRealtimeCollector()
		service.StartManagedPollingRealtimeCollector()
		if !strings.EqualFold(strings.TrimSpace(os.Getenv("ASSISTANT_WORKER_ENABLED")), "false") {
			configuredWorker, workerErr := assistantworker.NewDefault(model.DB, common.NodeName)
			switch {
			case workerErr == nil:
				aiAssistantWorker = configuredWorker
				aiAssistantWorker.Start()
				common.SysLog("AI assistant worker started")
			case errors.Is(workerErr, assistantsecrets.ErrKeyNotConfigured):
				common.SysLog("AI assistant worker disabled: configure ASSISTANT_SECRET_KEY(S) or MANAGED_INSTANCE_SECRET_KEY")
			default:
				common.SysError("AI assistant worker failed to initialize: " + workerErr.Error())
			}
		}
	}
	controlPlaneLeader := service.StartControlPlaneLeader(startControlPlaneServices, func() {
		select {
		case leadershipLost <- struct{}{}:
		default:
		}
	})

	var pprofServer *http.Server
	if os.Getenv("ENABLE_PPROF") == "true" {
		pprofServer = newPprofServer()
		go func() {
			if err := pprofServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("pprof server failed: %v", err)
			}
		}()
		go common.Monitor()
		common.SysLog("pprof enabled on 127.0.0.1:8005")
	}

	err = common.StartPyroScope()
	if err != nil {
		common.SysError(fmt.Sprintf("start pyroscope error : %v", err))
	}

	// Initialize HTTP server
	server := gin.New()
	if err := server.SetTrustedProxies(common.TrustedProxies()); err != nil {
		common.FatalLog("invalid TRUSTED_PROXIES: " + err.Error())
	}
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
	case <-leadershipLost:
		common.SysLog("control plane leadership was lost, shutting down this master process")
	}
	signal.Stop(quit)

	// SSE streams may run for minutes; give them time to finish before forced exit
	shutdownTimeout := time.Duration(shutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	controlPlaneMu.Lock()
	controlPlaneShuttingDown = true
	workerToStop := aiAssistantWorker
	controlPlaneMu.Unlock()
	if workerToStop != nil {
		if err := workerToStop.Stop(ctx); err != nil {
			common.SysError("AI assistant worker did not stop before shutdown deadline: " + err.Error())
		}
	}
	if err := service.StopManagedDashboardCollector(ctx); err != nil {
		common.SysError("managed dashboard collector did not stop before shutdown deadline: " + err.Error())
	}
	if err := service.StopManagedConductorRealtimeCollector(ctx); err != nil {
		common.SysError("managed conductor realtime collector did not stop before shutdown deadline: " + err.Error())
	}
	if err := service.StopManagedPollingRealtimeCollector(ctx); err != nil {
		common.SysError("managed polling realtime collector did not stop before shutdown deadline: " + err.Error())
	}
	if err := service.StopSystemTaskRunner(ctx); err != nil {
		common.SysError("system task runner did not stop before shutdown deadline: " + err.Error())
	}
	if err := controlPlaneLeader.Stop(ctx); err != nil {
		common.SysError("control plane leader did not stop before shutdown deadline: " + err.Error())
	}
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
	if pprofServer != nil {
		if err := pprofServer.Shutdown(ctx); err != nil {
			common.SysError("pprof server did not stop before shutdown deadline: " + err.Error())
		}
	}
	if err := service.StopSystemInstanceReporter(ctx); err != nil {
		common.SysError("system instance reporter did not stop before shutdown deadline: " + err.Error())
	}
	common.SysLog("server exited")
}

func runControlPlaneRepairs() error {
	migration, err := managedinstance.MigrateLegacyAlertRules()
	if err != nil {
		return fmt.Errorf("migrate managed instance alert rules: %w", err)
	}
	if migration.Instances > 0 {
		common.SysLog(fmt.Sprintf("managed instance alert rule migration completed: rules=%d instances=%d", migration.Rules, migration.Instances))
	}
	repair, err := billingalert.RepairLegacyDiscountRates()
	if err != nil {
		return fmt.Errorf("repair billing alert discount rates: %w", err)
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
	alertRepair, err := managedinstance.RepairAlertEventProjections()
	if err != nil {
		return fmt.Errorf("repair managed instance alert events: %w", err)
	}
	if alertRepair.Processed > 0 {
		common.SysLog(fmt.Sprintf("managed instance alert event repair completed: processed=%d", alertRepair.Processed))
	}
	return nil
}

func newPprofServer() *http.Server {
	pprofMux := http.NewServeMux()
	pprofMux.HandleFunc("/debug/pprof/", httppprof.Index)
	pprofMux.HandleFunc("/debug/pprof/cmdline", httppprof.Cmdline)
	pprofMux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
	pprofMux.HandleFunc("/debug/pprof/symbol", httppprof.Symbol)
	pprofMux.HandleFunc("/debug/pprof/trace", httppprof.Trace)
	return &http.Server{
		Addr:              "127.0.0.1:8005",
		Handler:           pprofMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
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
