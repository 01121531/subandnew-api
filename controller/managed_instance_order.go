package controller

import (
	"errors"
	"net/http"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
)

func requireInstanceOrderRoot(c *gin.Context) bool {
	if c.GetInt("role") >= common.RoleRootUser {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "root access is required to manage global instance order"})
	return false
}

func GetManagedInstanceOrder(c *gin.Context) {
	if !requireInstanceOrderRoot(c) {
		return
	}
	view, err := managedinstance.GetOrder()
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": view})
}

func PutManagedInstanceOrder(c *gin.Context) {
	if !requireInstanceOrderRoot(c) {
		return
	}
	var request struct {
		Version     int64   `json:"version"`
		InstanceIDs []int64 `json:"instance_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Version < 1 || request.InstanceIDs == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid instance order request"})
		return
	}
	view, err := managedinstance.SaveOrder(request.Version, request.InstanceIDs, c.GetInt("id"))
	if errors.Is(err, managedinstance.ErrInstanceOrderConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		return
	}
	if errors.Is(err, managedinstance.ErrInvalidInstanceOrder) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err != nil {
		managedInstanceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": view})
}
