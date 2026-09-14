package controller

import (
	"github.com/01121531/subandnew-api/model"
	"net/http"

	"github.com/01121531/subandnew-api/service/authz"

	"github.com/gin-gonic/gin"
)

// GetPermissionCatalog returns the permission schema used by the client to
// render the permission editor: the registry of resources with their actions
// and display label keys, plus the roles with their baseline grant matrices.
// Defining it in the authz package keeps the schema in a single place.
func GetPermissionCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"resources":   authz.Catalog(),
			"roles":       authz.Roles(),
			"data_fields": adminDataFieldCatalog(),
		},
	})
}

func adminDataFieldCatalog() []gin.H {
	items := make([]gin.H, 0, len(model.AdminDataFields))
	for _, key := range model.AdminDataFields {
		items = append(items, gin.H{"key": key, "label_key": "adminData.fields." + key})
	}
	return items
}
