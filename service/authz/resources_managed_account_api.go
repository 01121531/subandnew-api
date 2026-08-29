package authz

const (
	ResourceManagedAccountAPI = "managed_account_api"

	ManagedAccountAPIActionView   = "view"
	ManagedAccountAPIActionManage = "manage"
	ManagedAccountAPIActionAudit  = "audit"
)

var (
	ManagedAccountAPIView   = Permission{Resource: ResourceManagedAccountAPI, Action: ManagedAccountAPIActionView}
	ManagedAccountAPIManage = Permission{Resource: ResourceManagedAccountAPI, Action: ManagedAccountAPIActionManage}
	ManagedAccountAPIAudit  = Permission{Resource: ResourceManagedAccountAPI, Action: ManagedAccountAPIActionAudit}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceManagedAccountAPI,
		LabelKey: "Account data APIs",
		Actions: []ActionDefinition{
			{Action: ManagedAccountAPIActionView, LabelKey: "View account data APIs", DescriptionKey: "View external account data authorizations and key metadata."},
			{Action: ManagedAccountAPIActionManage, LabelKey: "Manage account data APIs", DescriptionKey: "Create, update, disable, rotate, and revoke external account data authorizations."},
			{Action: ManagedAccountAPIActionAudit, LabelKey: "Audit account data APIs", DescriptionKey: "View external account data API access logs."},
		},
	})
}
