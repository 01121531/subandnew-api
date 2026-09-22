package dailyreport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
)

type SupplierBill struct {
	InstanceID     int64              `json:"instance_id"`
	SupplierCode   string             `json:"supplier_code"`
	SupplierName   string             `json:"supplier_name"`
	BillDate       string             `json:"bill_date"`
	Timezone       string             `json:"timezone"`
	Requests       float64            `json:"requests"`
	InputTokens    float64            `json:"input_tokens"`
	OutputTokens   float64            `json:"output_tokens"`
	CacheTokens    float64            `json:"cache_tokens"`
	OriginalAmount float64            `json:"original_amount"`
	PayableAmount  float64            `json:"payable_amount"`
	Currency       string             `json:"currency"`
	TokenDetails   map[string]float64 `json:"token_details,omitempty"`
	ChannelCount   int                `json:"channel_count"`
	ObservedAt     int64              `json:"observed_at"`
	Status         string             `json:"status"`
	ErrorCode      string             `json:"error_code,omitempty"`
}

type SupplierBillsOverview struct {
	StartDate     string         `json:"start_date"`
	EndDate       string         `json:"end_date"`
	Timezone      string         `json:"timezone"`
	BillCount     int            `json:"bill_count"`
	TotalRequests float64        `json:"total_requests"`
	TotalPayable  float64        `json:"total_payable"`
	Items         []SupplierBill `json:"items"`
	CollectedAt   int64          `json:"collected_at"`
}

type UploadRecord struct {
	InstanceID  int64  `json:"instance_id"`
	ID          int64  `json:"id"`
	VendorID    string `json:"vendor_id"`
	Vendor      string `json:"vendor"`
	Batch       string `json:"batch"`
	Account     string `json:"account_identifier"`
	State       string `json:"state"`
	IssueType   string `json:"issue_type,omitempty"`
	Submitted   bool   `json:"submitted"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	BeijingDate string `json:"beijing_date"`
}

type UploadDay struct {
	Date        string         `json:"date"`
	UploadCount int            `json:"upload_count"`
	Submitted   int            `json:"submitted"`
	NeedsFix    int            `json:"needs_fix"`
	ByVendor    map[string]int `json:"by_vendor"`
	ByState     map[string]int `json:"by_state"`
}

type UploadsOverview struct {
	StartDate   string         `json:"start_date"`
	EndDate     string         `json:"end_date"`
	Timezone    string         `json:"timezone"`
	Days        []UploadDay    `json:"days"`
	Items       []UploadRecord `json:"items"`
	CollectedAt int64          `json:"collected_at"`
}

type SupplierOption struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func ValidateDateRange(startDate, endDate string) (Window, Window, error) {
	start, err := NaturalDay(startDate)
	if err != nil {
		return Window{}, Window{}, ErrInvalidRange
	}
	end, err := NaturalDay(endDate)
	if err != nil || end.End < start.Start || end.End-start.Start > 31*24*60*60-1 {
		return Window{}, Window{}, ErrInvalidRange
	}
	return start, end, nil
}

func ListClaudeGatewaySuppliers(ctx context.Context, instanceID int64) ([]SupplierOption, error) {
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, instanceID).Error; err != nil {
		return nil, err
	}
	if instance.Kind != model.ManagedInstanceKindClaudeGateway {
		return nil, errors.New("daily report supplier options require Claude Gateway")
	}
	seen := map[string]SupplierOption{}
	for page := 1; page <= 100; page++ {
		result, err := managedaccount.Execute(ctx, managedaccount.Query{
			InstanceIDs: []int64{instanceID}, Dataset: managedaccount.DatasetInventory,
			Page: page, PageSize: 100, AllowLargePage: true,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range result.Items {
			code := strings.TrimSpace(item.VendorID)
			name := strings.TrimSpace(item.VendorName)
			if code == "" {
				code = "__platform__"
			}
			if name == "" {
				name = "平台自有"
			}
			seen[code] = SupplierOption{Code: code, Name: name}
		}
		if !result.HasMore {
			break
		}
	}
	items := make([]SupplierOption, 0, len(seen))
	for _, item := range seen {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func CollectSupplierBills(ctx context.Context, instanceID int64, startDate, endDate string) (SupplierBillsOverview, error) {
	_, _, err := ValidateDateRange(startDate, endDate)
	if err != nil {
		return SupplierBillsOverview{}, err
	}
	instance, err := loadInstanceKind(instanceID, model.ManagedInstanceKindRouter)
	if err != nil {
		return SupplierBillsOverview{}, err
	}
	credential, connector, err := externalConnector(instance.Id)
	if err != nil {
		return SupplierBillsOverview{}, err
	}
	login, err := connector.DoJSON(ctx, http.MethodPost, "/api/user/login", nil, map[string]string{"username": credential.UserID, "password": credential.Secret})
	if err != nil || login.StatusCode >= 300 {
		return SupplierBillsOverview{}, errors.New("router login failed")
	}
	path := "/api/maas/supplier-bills/?start_date=" + url.QueryEscape(startDate) + "&end_date=" + url.QueryEscape(endDate)
	response, err := connector.DoJSON(ctx, http.MethodGet, path, nil, nil)
	if err != nil || response.StatusCode >= 300 {
		return SupplierBillsOverview{}, errors.New("router bills failed")
	}
	rows, err := decodeRouterRows(response.Body)
	if err != nil {
		return SupplierBillsOverview{}, err
	}
	items := make([]SupplierBill, 0, len(rows))
	for _, row := range rows {
		item := routerBill(row, instanceID, startDate)
		if item.BillDate < startDate || item.BillDate > endDate {
			continue
		}
		items = append(items, item)
	}
	if channelCount, channelErr := collectRouterChannelCount(ctx, connector); channelErr == nil {
		for index := range items {
			if items[index].ChannelCount == 0 {
				items[index].ChannelCount = channelCount
			}
		}
	}
	result := SupplierBillsOverview{StartDate: startDate, EndDate: endDate, Timezone: "Asia/Shanghai", Items: items, CollectedAt: time.Now().Unix()}
	result.BillCount = len(items)
	for _, item := range items {
		result.TotalRequests += item.Requests
		result.TotalPayable += item.PayableAmount
	}
	return result, nil
}

func CollectUploads(ctx context.Context, instanceID int64, startDate, endDate string) (UploadsOverview, error) {
	start, end, err := ValidateDateRange(startDate, endDate)
	if err != nil {
		return UploadsOverview{}, err
	}
	instance, err := loadInstanceKind(instanceID, model.ManagedInstanceKindNevermore)
	if err != nil {
		return UploadsOverview{}, err
	}
	credential, connector, err := externalConnector(instance.Id)
	if err != nil {
		return UploadsOverview{}, err
	}
	login, err := connector.DoJSON(ctx, http.MethodPost, "/admin/v1/auth/login", nil, map[string]string{"username": credential.UserID, "password": credential.Secret})
	if err != nil || login.StatusCode >= 300 {
		return UploadsOverview{}, errors.New("nevermore login failed")
	}
	items := make([]UploadRecord, 0)
	for offset := 0; offset < 100000; offset += 100 {
		path := "/supplier/v1/deliveries?limit=100&offset=" + strconv.Itoa(offset)
		response, requestErr := connector.DoJSON(ctx, http.MethodGet, path, nil, nil)
		if requestErr != nil || response.StatusCode >= 300 {
			return UploadsOverview{}, errors.New("nevermore deliveries failed")
		}
		page, total, decodeErr := decodeNevermorePage(response.Body)
		if decodeErr != nil {
			return UploadsOverview{}, decodeErr
		}
		for _, item := range page {
			parsed, parseErr := parseExternalTimestamp(item.CreatedAt)
			if parseErr != nil {
				continue
			}
			beijingDate := parsed.In(BeijingLocation()).Format("2006-01-02")
			if parsed.Unix() >= start.Start && parsed.Unix() <= end.End {
				item.InstanceID = instanceID
				item.BeijingDate = beijingDate
				items = append(items, item)
			}
		}
		if len(page) == 0 || offset+len(page) >= total || len(page) < 100 {
			break
		}
	}
	return buildUploadsOverview(startDate, endDate, items), nil
}

func parseExternalTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid external timestamp")
}

func buildUploadsOverview(startDate, endDate string, items []UploadRecord) UploadsOverview {
	result := UploadsOverview{StartDate: startDate, EndDate: endDate, Timezone: "Asia/Shanghai", Items: items, CollectedAt: time.Now().Unix()}
	byDate := map[string]*UploadDay{}
	for _, item := range items {
		day := byDate[item.BeijingDate]
		if day == nil {
			day = &UploadDay{Date: item.BeijingDate, ByVendor: map[string]int{}, ByState: map[string]int{}}
			byDate[item.BeijingDate] = day
		}
		day.UploadCount++
		day.ByVendor[item.Vendor]++
		day.ByState[item.State]++
		if item.Submitted {
			day.Submitted++
		}
		if item.State == "NEEDS_FIX" {
			day.NeedsFix++
		}
	}
	for _, day := range byDate {
		result.Days = append(result.Days, *day)
	}
	sort.Slice(result.Days, func(i, j int) bool { return result.Days[i].Date < result.Days[j].Date })
	return result
}

func loadInstanceKind(instanceID int64, kind string) (*model.ManagedInstance, error) {
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, instanceID).Error; err != nil {
		return nil, err
	}
	if instance.Kind != kind {
		return nil, fmt.Errorf("daily report requires instance kind %s", kind)
	}
	return &instance, nil
}

func externalConnector(instanceID int64) (*managedinstance.CredentialMaterial, *managedinstance.Connector, error) {
	credential, err := managedinstance.LoadCredentialForInstance(instanceID)
	if err != nil {
		return nil, nil, err
	}
	if credential == nil || credential.AuthType != "account_password" {
		return nil, nil, errors.New("account password credential required")
	}
	_, connector, err := managedinstance.NewConnectorForInstance(instanceID)
	return credential, connector, err
}

func decodeRouterRows(body []byte) ([]map[string]any, error) {
	var direct []map[string]any
	if json.Unmarshal(body, &direct) == nil {
		return direct, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errors.New("invalid router response")
	}
	for _, key := range []string{"data", "items", "results"} {
		raw, ok := envelope[key]
		if !ok {
			continue
		}
		var rows []map[string]any
		if json.Unmarshal(raw, &rows) == nil {
			return rows, nil
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil {
			for _, nestedKey := range []string{"items", "data", "results"} {
				if json.Unmarshal(nested[nestedKey], &rows) == nil {
					return rows, nil
				}
			}
		}
	}
	return []map[string]any{}, nil
}

func collectRouterChannelCount(ctx context.Context, connector *managedinstance.Connector) (int, error) {
	response, err := connector.DoJSON(ctx, http.MethodGet, "/api/channel/?tag_mode=false&id_sort=true&p=1&page_size=100", nil, nil)
	if err != nil || response.StatusCode >= 300 {
		return 0, errors.New("router channels failed")
	}
	rows, err := decodeRouterRows(response.Body)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func routerBill(row map[string]any, instanceID int64, fallbackDate string) SupplierBill {
	name := firstString(row, "supplier_name", "supplier", "vendor_name", "vendor", "username")
	code := firstString(row, "supplier_code", "supplier_id", "vendor_id", "vendor_code", "username")
	if code == "" {
		code = name
	}
	date := firstString(row, "bill_date", "date", "billing_date", "day")
	if len(date) >= 10 {
		date = date[:10]
	}
	if date == "" {
		date = fallbackDate
	}
	details := map[string]float64{}
	for _, key := range []string{"prompt_tokens", "input_tokens", "completion_tokens", "output_tokens", "cache_tokens", "cache_read_tokens", "cache_creation_tokens", "cache_creation_5m_tokens", "cache_creation_1h_tokens", "image_input_tokens", "audio_input_tokens"} {
		if value, ok := number(row, key); ok {
			details[key] = value
		}
	}
	return SupplierBill{
		InstanceID: instanceID, SupplierCode: code, SupplierName: name, BillDate: date,
		Timezone: firstStringDefault(row, "timezone", "UTC+8"), Requests: firstNumber(row, "request_count", "requests", "total_requests"),
		InputTokens: firstNumber(row, "prompt_tokens", "input_tokens"), OutputTokens: firstNumber(row, "completion_tokens", "output_tokens"),
		CacheTokens:    firstNumber(row, "cache_tokens", "cache_read_tokens", "cache_creation_tokens"),
		OriginalAmount: firstNumber(row, "raw_amount", "original_amount", "original_total_amount", "settlement_amount"),
		PayableAmount:  firstNumber(row, "actual_settlement_amount", "payable_amount", "pay_amount", "settlement_amount", "raw_amount"),
		Currency:       firstStringDefault(row, "currency", "USD"), TokenDetails: details,
		ChannelCount: int(firstNumber(row, "channel_count", "channels_count")), Status: "succeeded", ObservedAt: time.Now().Unix(),
	}
}

func decodeNevermorePage(body []byte) ([]UploadRecord, int, error) {
	var envelope struct {
		Items []UploadRecord `json:"items"`
		Total int            `json:"total"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, 0, errors.New("invalid nevermore response")
	}
	return envelope.Items, envelope.Total, nil
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstStringDefault(row map[string]any, key, fallback string) string {
	if value := firstString(row, key); value != "" {
		return value
	}
	return fallback
}

func number(row map[string]any, key string) (float64, bool) {
	value, ok := row[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func firstNumber(row map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := number(row, key); ok {
			return value
		}
	}
	return 0
}
