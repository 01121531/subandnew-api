package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
)

func ListManagedAccountFilterTemplates(c *gin.Context) {
	if !adminQueryAllowed(c) {
		return
	}
	templates, err := managedinstance.ListAccountFilterTemplates(c.GetInt("id"))
	if err != nil {
		managedAccountFilterTemplateError(c, err)
		return
	}
	visible := make([]*managedinstance.AccountFilterTemplateView, 0, len(templates))
	for _, template := range templates {
		if err := managedAccountFilterRulesAllowed(c, template.Rules); err != nil {
			if errors.Is(err, authz.ErrDataForbidden) {
				continue
			}
			adminDataError(c, err)
			return
		}
		visible = append(visible, template)
	}
	adminManagedInstanceDTOJSON(c, http.StatusOK, visible)
}

func CreateManagedAccountFilterTemplate(c *gin.Context) {
	var request managedinstance.AccountFilterTemplateInput
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid managed account filter template"})
		return
	}
	if err := managedAccountFilterRulesAllowed(c, request.Rules); err != nil {
		adminDataError(c, err)
		return
	}
	template, err := managedinstance.CreateAccountFilterTemplate(c.GetInt("id"), request)
	if err != nil {
		managedAccountFilterTemplateError(c, err)
		return
	}
	adminManagedInstanceDTOJSON(c, http.StatusCreated, template)
}

func UpdateManagedAccountFilterTemplate(c *gin.Context) {
	id, ok := managedAccountFilterTemplateID(c)
	if !ok {
		return
	}
	var request managedinstance.AccountFilterTemplateInput
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid managed account filter template"})
		return
	}
	if err := managedAccountFilterRulesAllowed(c, request.Rules); err != nil {
		adminDataError(c, err)
		return
	}
	template, err := managedinstance.UpdateAccountFilterTemplate(id, c.GetInt("id"), request)
	if err != nil {
		managedAccountFilterTemplateError(c, err)
		return
	}
	adminManagedInstanceDTOJSON(c, http.StatusOK, template)
}

func DeleteManagedAccountFilterTemplate(c *gin.Context) {
	id, ok := managedAccountFilterTemplateID(c)
	if !ok {
		return
	}
	if err := managedinstance.DeleteAccountFilterTemplate(id, c.GetInt("id")); err != nil {
		managedAccountFilterTemplateError(c, err)
		return
	}
	adminDataJSON(c, http.StatusOK, gin.H{"id": id})
}

func managedAccountFilterRulesAllowed(c *gin.Context, rules []managedinstance.AccountFilterRule) error {
	access := authz.DataAccessFrom(c.Request.Context())
	if access == nil {
		return nil
	}
	if err := access.Current(model.DB); err != nil {
		return err
	}
	for _, rule := range rules {
		if err := access.CheckDataField(strings.TrimSpace(rule.Field)); err != nil {
			return err
		}
	}
	return nil
}

func managedAccountFilterTemplateID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid managed account filter template id"})
		return 0, false
	}
	return id, true
}

func managedAccountFilterTemplateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, managedinstance.ErrInvalidAccountFilterTemplate):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrAccountFilterTemplateMissing):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, managedinstance.ErrAccountFilterTemplateConflict):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
	default:
		managedInstanceError(c, err)
	}
}
