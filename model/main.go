package model

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	DB             *gorm.DB
	commonGroupCol string
	lastPingTime   time.Time
	pingMutex      sync.Mutex
)

func initCol() {
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		commonGroupCol = `"group"`
		return
	}
	commonGroupCol = "`group`"
}

func createRootAccountIfNeed() error {
	var user User
	if err := DB.First(&user).Error; err == nil {
		return nil
	}

	common.SysLog("no user exists, create a root user for you: username is root, password is 123456")
	hashedPassword, err := common.Password2Hash("123456")
	if err != nil {
		return err
	}
	return DB.Create(&User{
		Username:    "root",
		Password:    hashedPassword,
		Role:        common.RoleRootUser,
		Status:      common.UserStatusEnabled,
		DisplayName: "Root User",
	}).Error
}

func CheckSetup() {
	setup := GetSetup()
	if setup != nil {
		common.SysLog("system is already initialized at: " + time.Unix(setup.InitializedAt, 0).String())
		constant.Setup = true
		return
	}

	if !RootUserExists() {
		common.SysLog("system is not initialized and no root user exists")
		constant.Setup = false
		return
	}

	common.SysLog("system is not initialized, but root user exists")
	if err := DB.Create(&Setup{Version: common.Version, InitializedAt: time.Now().Unix()}).Error; err != nil {
		common.SysLog("failed to create setup record: " + err.Error())
	}
	constant.Setup = true
}

func chooseDB() (*gorm.DB, common.DatabaseType, error) {
	dsn := os.Getenv("SQL_DSN")
	if dsn == "" || strings.HasPrefix(dsn, "local") {
		common.SysLog("SQL_DSN not set, using SQLite as database")
		db, err := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{PrepareStmt: true})
		return db, common.DatabaseTypeSQLite, err
	}

	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		common.SysLog("using PostgreSQL as database")
		db, err := gorm.Open(postgres.New(postgres.Config{
			DSN:                  dsn,
			PreferSimpleProtocol: true,
		}), &gorm.Config{PrepareStmt: true})
		return db, common.DatabaseTypePostgreSQL, err
	}

	if !strings.Contains(dsn, "parseTime") {
		if strings.Contains(dsn, "?") {
			dsn += "&parseTime=true"
		} else {
			dsn += "?parseTime=true"
		}
	}
	common.SysLog("using MySQL as database")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{PrepareStmt: true})
	return db, common.DatabaseTypeMySQL, err
}

// OpenDatabaseWithoutMigration is reserved for explicit offline maintenance
// commands. It opens the configured database without AutoMigrate or setup side
// effects so inventory and archival can run before any ordinary startup work.
func OpenDatabaseWithoutMigration() (*gorm.DB, common.DatabaseType, error) {
	return chooseDB()
}

func InitDB() error {
	db, dbType, err := chooseDB()
	if err != nil {
		return err
	}
	common.SetMainDatabaseType(dbType)
	initCol()
	if common.DebugEnabled {
		db = db.Debug()
	}
	DB = db

	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		if err := checkMySQLChineseSupport(DB); err != nil {
			return err
		}
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	if err := configureConnectionPool(sqlDB, dbType); err != nil {
		return err
	}
	if !common.IsMasterNode {
		return nil
	}

	common.SysLog("database migration started")
	return migrateDB()
}

func configureConnectionPool(sqlDB *sql.DB, databaseType common.DatabaseType) error {
	maxIdleConns := common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100)
	maxOpenConns := common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000)
	connectionLifetime := time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60))
	if databaseType == common.DatabaseTypeSQLite {
		maxOpenConns = common.GetEnvOrDefault("SQLITE_MAX_OPEN_CONNS", 1)
		if maxOpenConns < 1 {
			maxOpenConns = 1
		}
		maxIdleConns = maxOpenConns
		connectionLifetime = 0
	}
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetConnMaxLifetime(connectionLifetime)
	if databaseType != common.DatabaseTypeSQLite {
		return nil
	}

	busyTimeoutMs := common.GetEnvOrDefault("SQLITE_BUSY_TIMEOUT_MS", 30000)
	if busyTimeoutMs < 0 {
		busyTimeoutMs = 0
	}
	_, err := sqlDB.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMs))
	return err
}

func migrateDB() error {
	return DB.AutoMigrate(controlPlaneModels()...)
}

// controlPlaneModels is the only ordinary-startup migration allowlist.
// Existing legacy gateway and commercial tables are intentionally untouched.
func controlPlaneModels() []interface{} {
	return []interface{}{
		&User{},
		&PasskeyCredential{},
		&Option{},
		&Setup{},
		&TwoFA{},
		&TwoFABackupCode{},
		&CustomOAuthProvider{},
		&UserOAuthBinding{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
		&SystemTaskScopeLock{},
		&CasbinRule{},
		&AuthzRole{},
		&ManagedInstance{},
		&ManagedInstanceCredential{},
		&ManagedInstanceSnapshot{},
		&ManagedDashboardSnapshot{},
		&ManagedAccountSnapshot{},
		&ManagedRPMHistory{},
		&ManagedInstanceAlert{},
		&ManagedUsageExport{},
		&ManagedExportItem{},
		&ManagedInstanceAudit{},
		&ManagedInstanceOperation{},
		&ManagedInstanceOperationBatch{},
		&ManagedInstanceOperationBatchItem{},
		&ManagedConfigTemplate{},
		&ManagedInstanceConfigBinding{},
		&BillingFilterTemplate{},
		&BillingFilterTemplateVersion{},
		&MetricAlertRule{},
		&MetricAlertRuleInstance{},
		&MetricAlertCondition{},
		&MetricAlertState{},
		&BillingAlertRule{},
		&BillingAlertRuleInstance{},
		&BillingAlertThreshold{},
		&BillingCycleSnapshot{},
		&BillingEvaluationSnapshot{},
		&BillingAlertEvent{},
		&BillingEmailDelivery{},
		&ExchangeRate{},
		&ExchangeRateSetting{},
		&SMTPSetting{},
		&BillingAudit{},
		&BillingAlertExport{},
	}
}

func migrateDBFast() error {
	return migrateDB()
}

func closeDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func CloseDB() error {
	return closeDB(DB)
}

func checkMySQLChineseSupport(db *gorm.DB) error {
	var charset string
	var collation string
	row := db.Raw("SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = DATABASE()").Row()
	if err := row.Scan(&charset, &collation); err != nil {
		return fmt.Errorf("failed to read schema charset and collation: %w", err)
	}

	allowed := []string{"utf8mb4", "utf8", "gbk", "big5", "gb18030"}
	charset = strings.ToLower(charset)
	collation = strings.ToLower(collation)
	for _, candidate := range allowed {
		if charset == candidate && (collation == "" || strings.HasPrefix(collation, candidate+"_")) {
			return nil
		}
	}
	return fmt.Errorf("schema charset/collation %s/%s cannot reliably store Chinese text", charset, collation)
}

func PingDB() error {
	pingMutex.Lock()
	defer pingMutex.Unlock()
	if time.Since(lastPingTime) < 10*time.Second {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Printf("Error getting sql.DB from GORM: %v", err)
		return err
	}
	if err := sqlDB.Ping(); err != nil {
		log.Printf("Error pinging DB: %v", err)
		return err
	}
	lastPingTime = time.Now()
	common.SysLog("Database pinged successfully")
	return nil
}
