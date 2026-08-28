package authz

const (
	ResourceAssistant = "assistant"

	AssistantActionAccess = "access"
	AssistantActionManage = "manage"
	AssistantActionAudit  = "audit"
)

var (
	AssistantAccess = Permission{Resource: ResourceAssistant, Action: AssistantActionAccess}
	AssistantManage = Permission{Resource: ResourceAssistant, Action: AssistantActionManage}
	AssistantAudit  = Permission{Resource: ResourceAssistant, Action: AssistantActionAudit}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceAssistant,
		LabelKey: "AI Assistant",
		Actions: []ActionDefinition{
			{
				Action:         AssistantActionAccess,
				LabelKey:       "Use AI assistant",
				DescriptionKey: "Use bound assistant channels and approved read-only tools.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         AssistantActionManage,
				LabelKey:       "Manage AI assistant",
				DescriptionKey: "Configure assistant channels, model profiles, identities, and policies.",
			},
			{
				Action:         AssistantActionAudit,
				LabelKey:       "Audit AI assistant",
				DescriptionKey: "View assistant runs, tool calls, delivery status, and redacted audit records.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}
