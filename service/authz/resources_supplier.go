package authz

const ResourceSupplierManagement = "supplier_management"

var (
	SupplierView   = Permission{Resource: ResourceSupplierManagement, Action: "view"}
	SupplierManage = Permission{Resource: ResourceSupplierManagement, Action: "manage"}
	SupplierAudit  = Permission{Resource: ResourceSupplierManagement, Action: "audit"}
)

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceSupplierManagement, LabelKey: "Supplier management", Actions: []ActionDefinition{
		{Action: "view", LabelKey: "View suppliers", DescriptionKey: "View supplier identities and bindings."},
		{Action: "manage", LabelKey: "Manage suppliers", DescriptionKey: "Manage supplier permissions, credentials and sessions."},
		{Action: "audit", LabelKey: "Audit suppliers", DescriptionKey: "View supplier operation audit metadata."},
	}})
}
