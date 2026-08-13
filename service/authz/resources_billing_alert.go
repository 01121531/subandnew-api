package authz

const (
	ResourceBillingAlert = "billing_alert"

	BillingAlertActionView     = "view"
	BillingAlertActionManage   = "manage"
	BillingAlertActionSettings = "settings"
)

var (
	BillingAlertView     = Permission{Resource: ResourceBillingAlert, Action: BillingAlertActionView}
	BillingAlertManage   = Permission{Resource: ResourceBillingAlert, Action: BillingAlertActionManage}
	BillingAlertSettings = Permission{Resource: ResourceBillingAlert, Action: BillingAlertActionSettings}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceBillingAlert,
		LabelKey: "Billing alerts",
		Actions: []ActionDefinition{
			{
				Action: BillingAlertActionView, LabelKey: "View billing alerts",
				DescriptionKey: "View billing alert rules and records for accessible managed instances.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action: BillingAlertActionManage, LabelKey: "Manage billing alerts",
				DescriptionKey: "Create, update, delete, and evaluate billing alert rules.",
			},
			{
				Action: BillingAlertActionSettings, LabelKey: "Manage billing alert settings",
				DescriptionKey: "Manage exchange rates and SMTP delivery settings.",
			},
		},
	})
}
