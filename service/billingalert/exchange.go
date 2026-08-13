package billingalert

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const (
	ExchangeSourceECB         = "ecb"
	ExchangeSourceFrankfurter = "frankfurter"
)

var ErrExchangeRateUnavailable = errors.New("exchange rate unavailable")

type RateQuote struct {
	Base         string `json:"base"`
	Quote        string `json:"quote"`
	Rate         string `json:"rate"`
	ObservedDate string `json:"observed_date"`
	Source       string `json:"source"`
	Fallback     bool   `json:"fallback"`
}

type ExchangeSettingInput struct {
	Automatic   bool     `json:"automatic"`
	DefaultMode string   `json:"default_mode"`
	ManualRate  string   `json:"manual_rate"`
	UpdateTimes []string `json:"update_times"`
	Timezone    string   `json:"timezone"`
}

type RateProvider interface {
	Name() string
	Latest(ctx context.Context) (RateQuote, error)
}

type ECBProvider struct {
	Client  *http.Client
	BaseURL string
}

func (provider ECBProvider) Name() string { return ExchangeSourceECB }

func (provider ECBProvider) Latest(ctx context.Context) (RateQuote, error) {
	baseURL := strings.TrimRight(provider.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://data-api.ecb.europa.eu/service"
	}
	endpoint := baseURL + "/data/EXR/D.USD+CNY.EUR.SP00.A?lastNObservations=10&format=csvdata"
	body, err := fetchRateBody(ctx, provider.Client, endpoint)
	if err != nil {
		return RateQuote{}, err
	}
	return parseECBUSDToCNY(body)
}

type FrankfurterProvider struct {
	Client  *http.Client
	BaseURL string
}

func (provider FrankfurterProvider) Name() string { return ExchangeSourceFrankfurter }

func (provider FrankfurterProvider) Latest(ctx context.Context) (RateQuote, error) {
	baseURL := strings.TrimRight(provider.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.frankfurter.dev"
	}
	body, err := fetchRateBody(ctx, provider.Client, baseURL+"/v1/latest?base=USD&symbols=CNY")
	if err != nil {
		return RateQuote{}, err
	}
	var response struct {
		Base  string             `json:"base"`
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return RateQuote{}, ErrExchangeRateUnavailable
	}
	rate := response.Rates["CNY"]
	if !strings.EqualFold(response.Base, "USD") || rate <= 0 || response.Date == "" {
		return RateQuote{}, ErrExchangeRateUnavailable
	}
	return RateQuote{
		Base: "USD", Quote: "CNY", Rate: formatDecimal(new(big.Rat).SetFloat64(rate), MoneyScale),
		ObservedDate: response.Date, Source: provider.Name(),
	}, nil
}

func FetchLatestRate(ctx context.Context, providers ...RateProvider) (RateQuote, error) {
	for index, provider := range providers {
		if provider == nil {
			continue
		}
		quote, err := provider.Latest(ctx)
		if err != nil {
			continue
		}
		quote.Fallback = index > 0
		return quote, nil
	}
	return RateQuote{}, ErrExchangeRateUnavailable
}

func SaveExchangeRate(quote RateQuote) (*model.ExchangeRate, error) {
	if quote.Base != "USD" || quote.Quote != "CNY" || quote.Source == "" || quote.ObservedDate == "" {
		return nil, ErrExchangeRateUnavailable
	}
	parsedRate, err := parseNonNegativeDecimal(quote.Rate)
	if err != nil || parsedRate.Sign() == 0 {
		return nil, ErrExchangeRateUnavailable
	}
	record := &model.ExchangeRate{
		BaseCurrency: quote.Base, QuoteCurrency: quote.Quote, ObservedDate: quote.ObservedDate,
		Source: quote.Source, Rate: quote.Rate, Fallback: quote.Fallback, FetchedAt: common.GetTimestamp(),
	}
	err = model.DB.Where(
		"base_currency = ? AND quote_currency = ? AND observed_date = ? AND source = ?",
		record.BaseCurrency, record.QuoteCurrency, record.ObservedDate, record.Source,
	).Assign(map[string]any{
		"rate": record.Rate, "fallback": record.Fallback, "fetched_at": record.FetchedAt,
	}).FirstOrCreate(record).Error
	return record, err
}

func LatestStoredExchangeRate() (*model.ExchangeRate, error) {
	var rate model.ExchangeRate
	err := model.DB.Where("base_currency = ? AND quote_currency = ?", "USD", "CNY").
		Order("observed_date DESC, fetched_at DESC, id DESC").First(&rate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &rate, err
}

func ExchangeRateOnOrBefore(date string) (*model.ExchangeRate, error) {
	var rate model.ExchangeRate
	err := model.DB.Where("base_currency = ? AND quote_currency = ? AND observed_date <= ?", "USD", "CNY", date).
		Order("observed_date DESC, fallback ASC, fetched_at DESC, id DESC").First(&rate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &rate, err
}

func EnsureExchangeRateSetting() (*model.ExchangeRateSetting, error) {
	setting := &model.ExchangeRateSetting{ID: 1}
	err := model.DB.Where("id = ?", 1).Attrs(model.ExchangeRateSetting{
		Automatic: true, DefaultMode: ExchangeModeLatest,
		PrimarySource: ExchangeSourceECB, FallbackSource: ExchangeSourceFrankfurter,
		UpdateTimes: `["17:30"]`, Timezone: "Asia/Shanghai",
	}).FirstOrCreate(setting).Error
	return setting, err
}

func UpdateExchangeRateSetting(input ExchangeSettingInput, actorID int) (*model.ExchangeRateSetting, error) {
	if actorID <= 0 || !validExchangeMode(input.DefaultMode) {
		return nil, ErrInvalidBillingInput
	}
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return nil, ErrInvalidBillingInput
	}
	if input.DefaultMode == ExchangeModeManual {
		rate, err := parseNonNegativeDecimal(input.ManualRate)
		if err != nil || rate.Sign() == 0 {
			return nil, ErrInvalidBillingInput
		}
	}
	if len(input.UpdateTimes) == 0 || len(input.UpdateTimes) > 24 {
		return nil, ErrInvalidBillingInput
	}
	for _, value := range input.UpdateTimes {
		if _, err := time.Parse("15:04", value); err != nil {
			return nil, ErrInvalidBillingInput
		}
	}
	encodedTimes, _ := json.Marshal(input.UpdateTimes)
	setting, err := EnsureExchangeRateSetting()
	if err != nil {
		return nil, err
	}
	err = model.DB.Model(setting).Updates(map[string]any{
		"automatic": input.Automatic, "default_mode": input.DefaultMode,
		"manual_rate": strings.TrimSpace(input.ManualRate), "update_times": string(encodedTimes),
		"timezone": input.Timezone, "updated_by": actorID, "updated_at": common.GetTimestamp(),
	}).Error
	if err != nil {
		return nil, err
	}
	return EnsureExchangeRateSetting()
}

func RefreshExchangeRate(ctx context.Context, actorID int) (*model.ExchangeRate, error) {
	setting, err := EnsureExchangeRateSetting()
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	quote, fetchErr := FetchLatestRate(ctx,
		ECBProvider{},
		FrankfurterProvider{},
	)
	if fetchErr != nil {
		_ = model.DB.Model(setting).Updates(map[string]any{
			"last_attempt_at": now, "last_error_code": "exchange_rate_unavailable",
			"updated_by": actorID, "updated_at": now,
		}).Error
		return nil, fetchErr
	}
	rate, err := SaveExchangeRate(quote)
	if err != nil {
		return nil, err
	}
	if err := model.DB.Model(setting).Updates(map[string]any{
		"latest_rate_id": rate.ID, "last_attempt_at": now, "last_succeeded_at": now,
		"last_error_code": "", "updated_by": actorID, "updated_at": now,
	}).Error; err != nil {
		return nil, err
	}
	return rate, nil
}

func ListExchangeRates(limit int) ([]*model.ExchangeRate, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	var rates []*model.ExchangeRate
	err := model.DB.Order("observed_date DESC, fetched_at DESC, id DESC").Limit(limit).Find(&rates).Error
	return rates, err
}

func fetchRateBody(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" {
		return nil, ErrExchangeRateUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json,text/csv")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrExchangeRateUnavailable, response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 2<<20))
}

func parseECBUSDToCNY(body []byte) (RateQuote, error) {
	reader := csv.NewReader(strings.NewReader(string(body)))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil || len(rows) < 2 {
		return RateQuote{}, ErrExchangeRateUnavailable
	}
	headers := map[string]int{}
	for index, header := range rows[0] {
		headers[strings.ToUpper(strings.TrimSpace(header))] = index
	}
	currencyIndex, currencyOK := headers["CURRENCY"]
	dateIndex, dateOK := headers["TIME_PERIOD"]
	valueIndex, valueOK := headers["OBS_VALUE"]
	if !currencyOK || !dateOK || !valueOK {
		return RateQuote{}, ErrExchangeRateUnavailable
	}
	type observation struct{ date, currency, value string }
	observations := make([]observation, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if currencyIndex >= len(row) || dateIndex >= len(row) || valueIndex >= len(row) {
			continue
		}
		currency := strings.ToUpper(strings.TrimSpace(row[currencyIndex]))
		if currency != "USD" && currency != "CNY" {
			continue
		}
		observations = append(observations, observation{
			date: strings.TrimSpace(row[dateIndex]), currency: currency, value: strings.TrimSpace(row[valueIndex]),
		})
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].date > observations[j].date })
	byDate := map[string]map[string]string{}
	for _, item := range observations {
		if byDate[item.date] == nil {
			byDate[item.date] = map[string]string{}
		}
		byDate[item.date][item.currency] = item.value
	}
	for _, item := range observations {
		values := byDate[item.date]
		if values["USD"] == "" || values["CNY"] == "" {
			continue
		}
		usdPerEUR, usdErr := parseNonNegativeDecimal(values["USD"])
		cnyPerEUR, cnyErr := parseNonNegativeDecimal(values["CNY"])
		if usdErr != nil || cnyErr != nil || usdPerEUR.Sign() == 0 {
			continue
		}
		rate := new(big.Rat).Quo(cnyPerEUR, usdPerEUR)
		return RateQuote{
			Base: "USD", Quote: "CNY", Rate: formatDecimal(rate, MoneyScale),
			ObservedDate: item.date, Source: ExchangeSourceECB,
		}, nil
	}
	return RateQuote{}, ErrExchangeRateUnavailable
}
