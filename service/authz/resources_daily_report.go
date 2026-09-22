package authz

const ResourceDailyReport = "daily_report"

var (
	DailyReportView   = Permission{Resource: ResourceDailyReport, Action: "view"}
	DailyReportManage = Permission{Resource: ResourceDailyReport, Action: "manage"}
	DailyReportExport = Permission{Resource: ResourceDailyReport, Action: "export"}
	DailyReportSend   = Permission{Resource: ResourceDailyReport, Action: "send"}
)

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceDailyReport, LabelKey: "Daily reports", Actions: []ActionDefinition{
		{Action: "view", LabelKey: "View daily reports", DescriptionKey: "View daily report snapshots and platform inventory.", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: "manage", LabelKey: "Manage daily report rules", DescriptionKey: "Create and edit supplier report rules and schedules.", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: "export", LabelKey: "Export daily reports", DescriptionKey: "Export daily report data to Excel.", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: "send", LabelKey: "Send daily reports", DescriptionKey: "Configure and execute daily report email delivery.", DefaultRoles: []string{BuiltInRoleAdmin}},
	}})
}
