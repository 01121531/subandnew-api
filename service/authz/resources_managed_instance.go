package authz

const (
	ResourceManagedInstance = "managed_instance"

	ManagedInstanceActionView         = "view"
	ManagedInstanceActionCreate       = "create"
	ManagedInstanceActionUpdate       = "update"
	ManagedInstanceActionDelete       = "delete"
	ManagedInstanceActionOperate      = "operate"
	ManagedInstanceActionBatchOperate = "batch_operate"
	ManagedInstanceActionSecretRotate = "secret_rotate"
	ManagedInstanceActionAudit        = "audit"
)

var (
	ManagedInstanceView         = Permission{Resource: ResourceManagedInstance, Action: ManagedInstanceActionView}
	ManagedInstanceCreate       = Permission{Resource: ResourceManagedInstance, Action: ManagedInstanceActionCreate}
	ManagedInstanceUpdate       = Permission{Resource: ResourceManagedInstance, Action: ManagedInstanceActionUpdate}
	ManagedInstanceDelete       = Permission{Resource: ResourceManagedInstance, Action: ManagedInstanceActionDelete}
	ManagedInstanceOperate      = Permission{Resource: ResourceManagedInstance, Action: ManagedInstanceActionOperate}
	ManagedInstanceBatchOperate = Permission{Resource: ResourceManagedInstance, Action: ManagedInstanceActionBatchOperate}
	ManagedInstanceSecretRotate = Permission{Resource: ResourceManagedInstance, Action: ManagedInstanceActionSecretRotate}
	ManagedInstanceAudit        = Permission{Resource: ResourceManagedInstance, Action: ManagedInstanceActionAudit}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceManagedInstance,
		LabelKey: "Managed Instances",
		Actions: []ActionDefinition{
			{
				Action:         ManagedInstanceActionView,
				LabelKey:       "View managed instances",
				DescriptionKey: "View managed instance lists, health, and redacted details.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ManagedInstanceActionCreate,
				LabelKey:       "Create managed instances",
				DescriptionKey: "Register managed instances and their credentials.",
			},
			{
				Action:         ManagedInstanceActionUpdate,
				LabelKey:       "Update managed instances",
				DescriptionKey: "Update managed instance connection and check settings.",
			},
			{
				Action:         ManagedInstanceActionDelete,
				LabelKey:       "Delete managed instances",
				DescriptionKey: "Remove local management records without deleting remote instances.",
			},
			{
				Action:         ManagedInstanceActionOperate,
				LabelKey:       "Operate managed instances",
				DescriptionKey: "Run approved checks and remote operations on one instance.",
			},
			{
				Action:         ManagedInstanceActionBatchOperate,
				LabelKey:       "Batch operate managed instances",
				DescriptionKey: "Run approved remote operations across multiple instances.",
			},
			{
				Action:         ManagedInstanceActionSecretRotate,
				LabelKey:       "Rotate managed instance credentials",
				DescriptionKey: "Replace or revoke stored remote management credentials.",
			},
			{
				Action:         ManagedInstanceActionAudit,
				LabelKey:       "Audit managed instance operations",
				DescriptionKey: "View managed instance operation and credential audit details.",
			},
		},
	})
}
