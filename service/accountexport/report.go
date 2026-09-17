package accountexport

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
)

var (
	ErrNoAccounts          = errors.New("export_schedule_no_data")
	ErrAccountLimit        = errors.New("export_schedule_account_limit")
	ErrSnapshotUnavailable = errors.New("export_schedule_snapshot_unavailable")
	ErrSnapshotChanged     = errors.New("export_schedule_snapshot_changed")
)

type ReportPeriod struct {
	Kind      string `json:"kind"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

func (p ReportPeriod) Window(scheduledAt time.Time) (managedinstance.TimeWindow, error) {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return managedinstance.TimeWindow{}, err
	}
	local := scheduledAt.In(zone)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	end := start.AddDate(0, 0, 1).Add(-time.Second)
	switch p.Kind {
	case "", "last30":
		start = start.AddDate(0, 0, -29)
	case "last7":
		start = start.AddDate(0, 0, -6)
	case "today":
	case "yesterday":
		start, end = start.AddDate(0, 0, -1), start.Add(-time.Second)
	case "fixed":
		start, err = time.ParseInLocation("2006-01-02", p.StartDate, zone)
		if err != nil {
			return managedinstance.TimeWindow{}, ErrInvalidSchedule
		}
		end, err = time.ParseInLocation("2006-01-02", p.EndDate, zone)
		if err != nil || end.Before(start) {
			return managedinstance.TimeWindow{}, ErrInvalidSchedule
		}
		end = end.AddDate(0, 0, 1).Add(-time.Second)
	default:
		return managedinstance.TimeWindow{}, ErrInvalidSchedule
	}
	if start.Unix() <= 0 || !end.After(start) {
		return managedinstance.TimeWindow{}, ErrInvalidSchedule
	}
	return managedinstance.TimeWindow{Start: start.Unix(), End: end.Unix(), Timezone: "Asia/Shanghai"}, nil
}

type Config struct {
	Schedule   Schedule                                `json:"schedule"`
	Period     ReportPeriod                            `json:"period"`
	Scope      string                                  `json:"scope"`
	Query      managedaccount.Query                    `json:"query"`
	Accounts   []service.ManagedAccountExportItemInput `json:"accounts,omitempty"`
	Recipients []string                                `json:"recipients"`
	Locale     string                                  `json:"locale"`
}

type PreparedRun struct {
	Export       *service.PreparedManagedAccountExport
	MissingCount int
	Sources      []managedaccount.SourceStatus
	Stale        bool
}

func CheckOwner(ownerID int, config Config) (*authz.DataAccess, error) {
	access, err := authz.LoadDataAccess(model.DB, ownerID)
	if err != nil {
		return nil, err
	}
	if !access.Can(authz.ManagedInstanceUsageView) {
		return nil, authz.ErrDataForbidden
	}
	if len(config.Query.InstanceIDs) == 0 {
		return nil, ErrInvalidSchedule
	}
	if err = access.CheckInstances(config.Query.InstanceIDs); err != nil {
		return nil, err
	}
	for _, account := range config.Accounts {
		if err = access.CheckInstances([]int64{account.InstanceID}); err != nil {
			return nil, err
		}
	}
	if err = access.CheckDataField(config.Query.SortBy); err != nil {
		return nil, err
	}
	for _, rule := range config.Query.Rules {
		if err = access.CheckDataField(rule.Field); err != nil {
			return nil, err
		}
	}
	if err = access.CheckQuery(url.Values{"search": {config.Query.Search}, "keyword": {strings.Join(append(append([]string{}, config.Query.IncludeTerms...), config.Query.ExcludeTerms...), " ")}}); err != nil {
		return nil, err
	}
	return access, nil
}

// PrepareRun only reads successful local snapshots for account selection.
// The existing export worker remains responsible for period-specific usage.
func PrepareRun(ctx context.Context, ownerID int, config Config, scheduledAt time.Time) (*PreparedRun, error) {
	access, err := CheckOwner(ownerID, config)
	if err != nil {
		return nil, err
	}
	window, err := config.Period.Window(scheduledAt)
	if err != nil {
		return nil, err
	}
	query := config.Query
	if query.PresetDays == 0 {
		query.PresetDays = 30
	}
	query.Page, query.PageSize, query.AllowLargePage = 1, 10000, true
	query.SelectedAccounts = nil
	switch config.Scope {
	case "dynamic":
	case "fixed":
		if len(config.Accounts) == 0 || len(config.Accounts) > 10000 {
			return nil, ErrAccountLimit
		}
		query.Search, query.IncludeTerms, query.ExcludeTerms, query.Rules = "", nil, nil, nil
		seen := make(map[managedaccount.AccountIdentity]bool)
		for _, account := range config.Accounts {
			identity := managedaccount.AccountIdentity{InstanceID: account.InstanceID, AccountID: strings.TrimSpace(account.AccountID)}
			inScope := false
			for _, id := range query.InstanceIDs {
				inScope = inScope || id == identity.InstanceID
			}
			if !inScope || identity.AccountID == "" || seen[identity] {
				return nil, ErrInvalidSchedule
			}
			seen[identity] = true
			query.SelectedAccounts = append(query.SelectedAccounts, identity)
		}
	default:
		return nil, ErrInvalidSchedule
	}
	query, err = managedaccount.NormalizeQuery(query)
	if err != nil {
		return nil, err
	}
	before, err := snapshotFingerprint(query.InstanceIDs)
	if err != nil {
		return nil, err
	}
	result, err := managedaccount.Execute(authz.WithDataAccess(ctx, access), query)
	if err != nil {
		return nil, err
	}
	for _, source := range result.Sources {
		if source.ObservedAt <= 0 {
			return nil, ErrSnapshotUnavailable
		}
	}
	if result.Total > 10000 || result.HasMore {
		return nil, ErrAccountLimit
	}
	if len(result.Items) == 0 {
		return nil, ErrNoAccounts
	}
	items := make([]service.ManagedAccountExportItemInput, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, service.ManagedAccountExportItemInput{InstanceID: item.InstanceID, AccountID: item.AccountID})
	}
	filter, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	accountRange, err := service.NormalizeManagedAccountRange(query.PresetDays, 0, 0, "Asia/Shanghai")
	if err != nil {
		return nil, err
	}
	export, err := service.PrepareManagedAccountExport(ownerID, service.ManagedAccountExportRequest{
		Source: query.Dataset, RangeKey: accountRange.RangeKey, Window: window, Locale: config.Locale,
		Search: query.Search, SortBy: query.SortBy, SortOrder: query.SortOrder, FilterSnapshot: filter, Items: items,
	})
	if err != nil {
		return nil, err
	}
	if err = access.Current(model.DB); err != nil {
		return nil, err
	}
	after, err := snapshotFingerprint(query.InstanceIDs)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, ErrSnapshotChanged
	}
	missing := 0
	if config.Scope == "fixed" {
		missing = len(query.SelectedAccounts) - len(items)
	}
	return &PreparedRun{Export: export, MissingCount: missing, Sources: result.Sources, Stale: result.Stale}, nil
}

func snapshotFingerprint(instanceIDs []int64) ([32]byte, error) {
	var rows []struct {
		ID            int64
		ObservedAt    int64
		Payload       string
		ETag          string
		SchemaVersion int
	}
	err := model.DB.Model(&model.ManagedAccountSnapshot{}).Select("id, observed_at, payload, etag, schema_version").Where("instance_id IN ?", instanceIDs).Order("id ASC").Find(&rows).Error
	if err != nil {
		return [32]byte{}, err
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
