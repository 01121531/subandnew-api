package authz

const ResourceMailboxManagement = "mailbox_management"

var (
	MailboxView        = Permission{ResourceMailboxManagement, "view"}
	MailboxManage      = Permission{ResourceMailboxManagement, "manage"}
	MailboxAssign      = Permission{ResourceMailboxManagement, "assign"}
	MailboxReview      = Permission{ResourceMailboxManagement, "review"}
	MailboxCredentials = Permission{ResourceMailboxManagement, "credentials"}
	MailboxOperators   = Permission{ResourceMailboxManagement, "operators"}
	MailboxAudit       = Permission{ResourceMailboxManagement, "audit"}
)

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceMailboxManagement, LabelKey: "mailbox.permissions.title", Actions: []ActionDefinition{
		{Action: "view", LabelKey: "mailbox.permissions.view", DescriptionKey: "mailbox.permissions.view"},
		{Action: "manage", LabelKey: "mailbox.permissions.manage", DescriptionKey: "mailbox.permissions.manage"},
		{Action: "assign", LabelKey: "mailbox.permissions.assign", DescriptionKey: "mailbox.permissions.assign"},
		{Action: "review", LabelKey: "mailbox.permissions.review", DescriptionKey: "mailbox.permissions.review"},
		{Action: "credentials", LabelKey: "mailbox.permissions.credentials", DescriptionKey: "mailbox.permissions.credentials"},
		{Action: "operators", LabelKey: "mailbox.permissions.operators", DescriptionKey: "mailbox.permissions.operators"},
		{Action: "audit", LabelKey: "mailbox.permissions.audit", DescriptionKey: "mailbox.permissions.audit"},
	}})
}
