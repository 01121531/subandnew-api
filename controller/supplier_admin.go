package controller

import (
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/supplier"
	"github.com/gin-gonic/gin"
)

func ListSuppliers(c *gin.Context) {
	page, size, ok := supplierPage(c)
	if !ok {
		return
	}
	items, total, err := supplierService().List(page, size)
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
	item, err := supplierService().Save(id, in)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	c.Set("supplier_id", item.ID)
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
	items := []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
	}{}
	if err := model.DB.Model(&model.ManagedInstance{}).Select("id, name, kind").Where("kind = ?", model.ManagedInstanceKindClaudeGateway).Order("name ASC").Find(&items).Error; err != nil {
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
	item, err := supplierService().SaveBinding(c.Request.Context(), id, bindingID, in)
	if err != nil {
		supplierFailure(c, err)
		return
	}
	c.Set("supplier_binding_id", item.ID)
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
	q := model.DB.Model(&model.SupplierAudit{}).Where("supplier_id = ?", id)
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
