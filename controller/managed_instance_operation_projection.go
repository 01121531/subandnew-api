package controller

import (
	"errors"
	"net/http"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
)

func adminManagedInstanceDTOJSON(c *gin.Context, status int, value any) {
	access := authz.DataAccessFrom(c.Request.Context())
	if access != nil {
		if err := access.Current(model.DB); err != nil {
			adminDataError(c, err)
			return
		}
	}
	data, err := managedinstance.ProjectManagedInstanceDTO(access, value)
	if err != nil {
		if errors.Is(err, authz.ErrDataForbidden) {
			adminDataError(c, err)
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "message": "data_projection_failed"})
		}
		return
	}
	// This typed projector is the final field boundary. Re-running the generic
	// registry would remove the structural keys that this DTO contract preserves.
	c.JSON(status, gin.H{"success": true, "message": "", "data": data})
}

// Use this after the task endpoint's existing type, owner, and instance checks.
func adminManagedInstanceTaskJSON(c *gin.Context, status int, task *model.SystemTask) {
	adminManagedInstanceDTOJSON(c, status, task.ToResponse())
}
