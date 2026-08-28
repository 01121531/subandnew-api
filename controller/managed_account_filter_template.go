package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/gin-gonic/gin"
)

func ListManagedAccountFilterTemplates(c *gin.Context) {
	templates, err := managedinstance.ListAccountFilterTemplates(c.GetInt("id"))
	if err != nil {
		managedAccountFilterTemplateError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": templates})
}

func CreateManagedAccountFilterTemplate(c *gin.Context) {
	var request managedinstance.AccountFilterTemplateInput
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid managed account filter template"})
		return
	}
	template, err := managedinstance.CreateAccountFilterTemplate(c.GetInt("id"), request)
	if err != nil {
		managedAccountFilterTemplateError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": template})
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
	template, err := managedinstance.UpdateAccountFilterTemplate(id, c.GetInt("id"), request)
	if err != nil {
		managedAccountFilterTemplateError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": template})
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
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": id}})
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
