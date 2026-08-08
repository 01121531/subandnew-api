package authz

const (
	ResourceManagedTemplate = "managed_template"

	ManagedTemplateActionView  = "view"
	ManagedTemplateActionApply = "apply"
)

var (
	ManagedTemplateView  = Permission{Resource: ResourceManagedTemplate, Action: ManagedTemplateActionView}
	ManagedTemplateApply = Permission{Resource: ResourceManagedTemplate, Action: ManagedTemplateActionApply}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceManagedTemplate,
		LabelKey: "Managed configuration",
		Actions: []ActionDefinition{
			{
				Action:         ManagedTemplateActionView,
				LabelKey:       "View configuration templates",
				DescriptionKey: "View whitelisted templates, bindings, and configuration drift.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ManagedTemplateActionApply,
				LabelKey:       "Apply configuration templates",
				DescriptionKey: "Create templates and apply reviewed configuration changes to managed instances.",
			},
		},
	})
}
