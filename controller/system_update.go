package controller

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/pkg/systemupdate"

	"github.com/gin-gonic/gin"
)

type startSystemUpdateRequest struct {
	ReleaseID int64 `json:"release_id" binding:"required"`
}

func GetSystemUpdateCapability(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    systemUpdateCapability(),
	})
}

func GetLatestSystemUpdate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	release, err := systemupdate.CheckLatest(ctx, common.Version)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if capability := systemUpdateCapability(); !capability.Supported {
		release.Installable = false
		release.Reason = capability.Reason
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    release,
	})
}

func GetSystemUpdateStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    systemupdate.GetState(),
	})
}

func StartSystemUpdate(c *gin.Context) {
	if capability := systemUpdateCapability(); !capability.Supported {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "online update is unavailable: " + capability.Reason,
			"data":    capability,
		})
		return
	}
	var request startSystemUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.ReleaseID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "release_id is required",
		})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	state, err := systemupdate.BeginUpdate(ctx, request.ReleaseID, common.Version, systemUpdatePort())
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already in progress") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{
			"success": false,
			"message": err.Error(),
			"data":    state,
		})
		return
	}
	recordManageAudit(c, "system_update.start", map[string]interface{}{
		"task_id":        state.TaskID,
		"from_version":   state.CurrentVersion,
		"target_version": state.TargetVersion,
		"release_id":     state.ReleaseID,
	})
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "",
		"data":    state,
	})
}

func systemUpdateCapability() systemupdate.Capability {
	capability := systemupdate.CapabilityStatus()
	if !capability.Supported {
		return capability
	}
	if !common.IsMasterNode {
		capability.Supported = false
		capability.Reason = "not_master_node"
		return capability
	}
	instances, err := model.ListSystemInstances()
	if err != nil {
		capability.Supported = false
		capability.Reason = "instance_detection_failed"
		return capability
	}
	now := common.GetTimestamp()
	activeInstances := 0
	for _, instance := range instances {
		if now-instance.LastSeenAt <= model.SystemInstanceStaleAfterSeconds {
			activeInstances++
		}
	}
	if activeInstances > 1 {
		capability.Supported = false
		capability.Reason = "multi_instance_deployment"
	}
	return capability
}

func systemUpdatePort() string {
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return port
	}
	return strconv.Itoa(*common.Port)
}
