package controller

import (
	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/supplier"
	"github.com/gin-gonic/gin"
	"strconv"
)

func ListSuppliers(c *gin.Context) {
	page, size, ok := supplierPage(c)
	if !ok {
		return
	}
	items, total, err := supplierService().ListOwned(c.GetInt("id"), page, size)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, gin.H{"items": items, "total": total})
}
func GetSupplier(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	item, err := supplierService().Supplier(id)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, item)
}
func SaveSupplier(c *gin.Context) {
	var id int64
	if c.Param("id") != "" {
		id = supplierID(c, "id")
		if id == 0 {
			return
		}
	}
	var in supplier.SupplierInput
	if !supplierDecode(c, &in) {
		return
	}
	item, err := supplierService().SaveOwned(c.Request.Context(), c.GetInt("id"), id, in)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	c.Set("supplier_id", item.ID)
	c.Set("supplier_policy_changes", item.PolicyChanges)
	c.Set("supplier_naming_changes", item.NamingChanges)
	supplierSuccess(c, item)
}
func DeleteSupplier(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	if err := supplierService().Delete(id); err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, gin.H{"completed": true})
}
func ResetSupplierPassword(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !supplierDecode(c, &in) {
		return
	}
	if err := supplierService().Password(id, in.Password, "", false); err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, gin.H{"completed": true})
}
func RevokeSupplierSessions(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	if err := supplierService().Revoke(id); err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, gin.H{"completed": true})
}
func ListSupplierInstances(c *gin.Context) {
	a, err := authz.LoadDataAccess(model.DB, c.GetInt("id"))
	if err != nil {
		supplierFailure(c, &supplier.Error{Status: 403, Code: "supplier_permission_denied"})
		return
	}
	items := []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
	}{}
	if err := a.ScopeQuery(model.DB.Model(&model.ManagedInstance{}), "id").Select("id, name, kind").Where("kind = ?", model.ManagedInstanceKindClaudeGateway).Order(model.ManagedInstanceOrderSQL).Find(&items).Error; err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, items)
}
func ListSupplierBindings(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	if _, err := supplierService().Supplier(id); err != nil {
		supplierFailure(c, err)
		return
	}
	items, err := supplierService().Bindings(id, false)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, items)
}
func SaveSupplierBinding(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	var bindingID int64
	if c.Param("binding_id") != "" {
		bindingID = supplierID(c, "binding_id")
		if bindingID == 0 {
			return
		}
	}
	var in supplier.BindingInput
	if !supplierDecode(c, &in) {
		return
	}
	if err := supplierService().CheckBindingGrant(c.GetInt("id"), id, bindingID, in.InstanceID); err != nil {
		supplierFailure(c, err)
		return
	}
	item, err := supplierService().SaveBinding(c.Request.Context(), id, bindingID, in)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	c.Set("supplier_binding_id", item.ID)
	c.Set("supplier_policy_changes", item.PolicyChanges)
	c.Set("supplier_naming_changes", item.NamingChanges)
	supplierSuccess(c, item)
}

func GetSupplierDefaultPolicy(c *gin.Context) {
	item, err := supplierService().DefaultPolicy()
	if err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, item)
}
func SaveSupplierDefaultPolicy(c *gin.Context) {
	var in struct {
		Policy   model.SupplierPolicy `json:"policy"`
		Revision int64                `json:"revision"`
	}
	if !supplierDecode(c, &in) {
		return
	}
	item, changes, err := supplierService().SaveDefaultPolicy(in.Policy, in.Revision)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	c.Set("supplier_policy_changes", changes)
	supplierSuccess(c, item)
}
func DeleteSupplierBinding(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	bindingID := supplierID(c, "binding_id")
	if bindingID == 0 {
		return
	}
	c.Set("supplier_binding_id", bindingID)
	if err := supplierService().DeleteBinding(id, bindingID); err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, gin.H{"completed": true})
}
func TestSupplierBinding(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	bindingID := supplierID(c, "binding_id")
	if bindingID == 0 {
		return
	}
	c.Set("supplier_binding_id", bindingID)
	if err := supplierService().TestBinding(c.Request.Context(), id, bindingID); err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, gin.H{"ok": true})
}
func ListSupplierAudits(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	page, size, ok := supplierPage(c)
	if !ok {
		return
	}
	items := []model.SupplierAudit{}
	var total int64
	q := model.DB.Model(&model.SupplierAudit{}).Where("supplier_id = ? OR (supplier_id = 0 AND action = ?)", id, "PUT /api/suppliers/default-policy")
	if a := authz.DataAccessFrom(c.Request.Context()); a != nil && a.Role < common.RoleRootUser {
		q = model.DB.Model(&model.SupplierAudit{}).Where("supplier_id = ?", id)
	}
	if err := q.Count(&total).Error; err != nil {
		supplierFailure(c, err)
		return
	}
	if err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, gin.H{"items": items, "total": total})
}

func SupplierAdminAccess(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	bindingID, _ := strconv.ParseInt(c.Param("binding_id"), 10, 64)
	a, err := supplierService().CheckAdmin(c.GetInt("id"), id, bindingID)
	if err != nil {
		supplierFailure(c, err)
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(authz.WithDataAccess(c.Request.Context(), a))
	c.Next()
}

func SupplierRootOnly(c *gin.Context) {
	a, err := authz.LoadDataAccess(model.DB, c.GetInt("id"))
	if err != nil || a.Role < common.RoleRootUser {
		supplierFailure(c, &supplier.Error{Status: 403, Code: "supplier_permission_denied"})
		c.Abort()
		return
	}
	c.Next()
}

func TakeoverSupplier(c *gin.Context) {
	id := supplierID(c, "id")
	if id == 0 {
		return
	}
	if err := supplierService().Takeover(c.Request.Context(), id, c.GetInt("id"), c.ClientIP()); err != nil {
		supplierFailure(c, err)
		return
	}
	supplierSuccess(c, gin.H{"completed": true})
}
