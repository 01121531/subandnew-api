package controller

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/01121531/subandnew-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	setupMutex                 sync.Mutex
	errSetupAlreadyInitialized = errors.New("system is already initialized")
)

type Setup struct {
	Status       bool   `json:"status"`
	RootInit     bool   `json:"root_init"`
	DatabaseType string `json:"database_type"`
}

type SetupRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
	SetupToken      string `json:"setup_token"`
}

func GetSetup(c *gin.Context) {
	result := Setup{Status: constant.Setup}
	if !constant.Setup {
		result.RootInit = model.RootUserExists()
		result.DatabaseType = string(common.MainDatabaseType())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func PostSetup(c *gin.Context) {
	setupMutex.Lock()
	defer setupMutex.Unlock()
	if constant.Setup {
		common.ApiErrorMsg(c, "system is already initialized")
		return
	}

	var request SetupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "invalid setup request")
		return
	}
	if !setupRequestAuthorized(c, request.SetupToken) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "setup authorization required"})
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if err := initializeSystem(request); err != nil {
		if errors.Is(err, errSetupAlreadyInitialized) {
			constant.Setup = true
			common.ApiErrorMsg(c, errSetupAlreadyInitialized.Error())
			return
		}
		common.ApiError(c, err)
		return
	}
	constant.Setup = true
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "system initialized"})
}

func setupRequestAuthorized(c *gin.Context, requestToken string) bool {
	configuredToken := strings.TrimSpace(os.Getenv("SETUP_TOKEN"))
	if configuredToken != "" {
		providedToken := strings.TrimSpace(c.GetHeader("X-Setup-Token"))
		if providedToken == "" {
			providedToken = strings.TrimSpace(requestToken)
		}
		configuredHash := sha256.Sum256([]byte(configuredToken))
		providedHash := sha256.Sum256([]byte(providedToken))
		return subtle.ConstantTimeCompare(configuredHash[:], providedHash[:]) == 1
	}
	return isDirectLoopbackSetupRequest(c.Request)
}

func isDirectLoopbackSetupRequest(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP"} {
		if strings.TrimSpace(request.Header.Get(header)) != "" {
			return false
		}
	}
	return true
}

func initializeSystem(request SetupRequest) error {
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var existing model.Setup
		if err := tx.First(&existing).Error; err == nil {
			return errSetupAlreadyInitialized
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		setup := model.Setup{ID: 1, Version: common.Version, InitializedAt: time.Now().Unix()}
		if err := tx.Create(&setup).Error; err != nil {
			return err
		}

		var rootCount int64
		if err := tx.Model(&model.User{}).Where("role = ?", common.RoleRootUser).Count(&rootCount).Error; err != nil {
			return err
		}
		if rootCount > 0 {
			return nil
		}
		if request.Username == "" || len(request.Username) > model.UserNameMaxLength {
			return errors.New("invalid administrator username")
		}
		if request.Password != request.ConfirmPassword || len(request.Password) < 8 || len(request.Password) > 20 {
			return errors.New("passwords must match and contain 8 to 20 characters")
		}
		hash, err := common.Password2Hash(request.Password)
		if err != nil {
			return err
		}
		return tx.Create(&model.User{
			Username: request.Username, Password: hash, DisplayName: "Root User",
			Role: common.RoleRootUser, Status: common.UserStatusEnabled,
		}).Error
	})
	if err != nil && model.GetSetup() != nil {
		return errSetupAlreadyInitialized
	}
	return err
}
