package controller

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-gonic/gin"
)

func adminDataError(c *gin.Context, err error) {
	status := http.StatusForbidden
	if err == authz.ErrAuthorizationChanged {
		status = http.StatusUnauthorized
	}
	c.AbortWithStatusJSON(status, gin.H{"success": false, "message": err.Error()})
}

func adminInstancesAllowed(c *gin.Context, ids []int64) bool {
	if err := authz.CheckContextInstances(c.Request.Context(), ids...); err != nil {
		adminDataError(c, err)
		return false
	}
	return true
}

func adminDataJSON(c *gin.Context, status int, data any) {
	if a := authz.DataAccessFrom(c.Request.Context()); a != nil {
		if err := a.Current(model.DB); err != nil {
			adminDataError(c, err)
			return
		}
		var err error
		data, err = a.Project(data)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{"success": false, "message": "data_projection_failed"})
			return
		}
	}
	c.JSON(status, gin.H{"success": true, "message": "", "data": data})
}

func adminQueryAllowed(c *gin.Context) bool {
	a := authz.DataAccessFrom(c.Request.Context())
	if a == nil {
		return true
	}
	for key, values := range c.Request.URL.Query() {
		if strings.Join(values, "") == "" {
			continue
		}
		if key == "sort" || key == "sort_by" {
			for _, value := range values {
				if err := a.CheckDataField(value); err != nil {
					adminDataError(c, err)
					return false
				}
			}
		}
		if authz.DataFieldGroup(key) != "" {
			if err := a.CheckDataField(key); err != nil {
				adminDataError(c, err)
				return false
			}
		}
		if (key == "search" || key == "keyword") && c.FullPath() != "/api/managed-instances" && (!a.HasField("email") || !a.HasField("vendor")) {
			adminDataError(c, authz.ErrDataForbidden)
			return false
		}
	}
	return true
}

func adminEventPayload(c *gin.Context, value any) ([]byte, error) {
	if a := authz.DataAccessFrom(c.Request.Context()); a != nil {
		if err := a.Current(model.DB); err != nil {
			return nil, err
		}
		var err error
		value, err = a.Project(value)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(value)
}

func adminStreamCurrent(c *gin.Context) bool {
	if a := authz.DataAccessFrom(c.Request.Context()); a != nil && a.Current(model.DB) != nil {
		_, _ = c.Writer.WriteString("event: authorization_revoked\ndata: {}\n\n")
		c.Writer.Flush()
		return false
	}
	return true
}

func authorizedRealtimeIDs(c *gin.Context) ([]int64, error) {
	ids, err := managedInstanceRealtimeIDs(c.Query("ids"))
	if err != nil {
		return nil, err
	}
	if err := authz.CheckContextInstances(c.Request.Context(), ids...); err != nil {
		return nil, err
	}
	return ids, nil
}
