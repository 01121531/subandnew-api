package controller

import (
	"github.com/01121531/subandnew-api/service/supplier"
	"github.com/gin-gonic/gin"
)

func GetSupplierPortalSettings(c *gin.Context) {
	settings, err := supplierService().PortalSettings()
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, settings)
}

func SaveSupplierPortalSettings(c *gin.Context) {
	var in supplier.PortalSettings
	if !supplierDecode(c, &in) {
		return
	}
	settings, changes, err := supplierService().SavePortalSettings(in)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	c.Set("supplier_policy_changes", changes)
	supplierSuccess(c, settings)
}
