package controller

import (
	"net/http"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/constant"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/gin-gonic/gin"
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
	if constant.Setup {
		common.ApiErrorMsg(c, "system is already initialized")
		return
	}

	var request SetupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "invalid setup request")
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if !model.RootUserExists() {
		if request.Username == "" || len(request.Username) > model.UserNameMaxLength {
			common.ApiErrorMsg(c, "invalid administrator username")
			return
		}
		if request.Password != request.ConfirmPassword || len(request.Password) < 8 || len(request.Password) > 20 {
			common.ApiErrorMsg(c, "passwords must match and contain 8 to 20 characters")
			return
		}
		hash, err := common.Password2Hash(request.Password)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		root := model.User{
			Username: request.Username, Password: hash, DisplayName: "Root User",
			Role: common.RoleRootUser, Status: common.UserStatusEnabled,
		}
		if err := model.DB.Create(&root).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	}

	setup := model.Setup{Version: common.Version, InitializedAt: time.Now().Unix()}
	if err := model.DB.Create(&setup).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	constant.Setup = true
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "system initialized"})
}
