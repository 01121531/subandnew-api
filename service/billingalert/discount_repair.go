package billingalert

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const discountRepairBatchSize = 500

type DiscountRepairResult struct {
	Rules       int `json:"rules"`
	Cycles      int `json:"cycles"`
	Evaluations int `json:"evaluations"`
	Events      int `json:"events"`
	Invalid     int `json:"invalid"`
}

func (result *DiscountRepairResult) CorrectedTotal() int {
	if result == nil {
		return 0
	}
	return result.Rules + result.Cycles + result.Evaluations + result.Events
}

// RepairLegacyDiscountRates is intentionally idempotent. Values in (1, 100]
// are legacy percentages; canonical multipliers in [0, 1] are untouched.
func RepairLegacyDiscountRates() (*DiscountRepairResult, error) {
	result := &DiscountRepairResult{}
	if model.DB == nil {
		return result, errors.New("database is not initialized")
	}
	if err := repairRuleDiscountRates(result); err != nil {
		return nil, err
	}
	if err := repairEvaluationDiscountRates(result); err != nil {
		return nil, err
	}
	if err := repairEventDiscountRates(result); err != nil {
		return nil, err
	}
	if err := repairCycleDiscountRates(result); err != nil {
		return nil, err
	}
	return result, nil
}

func repairRuleDiscountRates(result *DiscountRepairResult) error {
	var rules []*model.BillingAlertRule
	if err := model.DB.Order("id ASC").Find(&rules).Error; err != nil {
		return err
	}
	for _, rule := range rules {
		normalized, changed, invalid := legacyDiscountRate(rule.DiscountRate)
		if invalid {
			result.Invalid++
			continue
		}
		if !changed {
			continue
		}
		if err := model.DB.Model(rule).Updates(map[string]any{
			"discount_rate": normalized,
			"updated_at":    common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
		result.Rules++
	}
	return nil
}

func repairEvaluationDiscountRates(result *DiscountRepairResult) error {
	var lastID int64
	for {
		var rows []*model.BillingEvaluationSnapshot
		if err := model.DB.Where("id > ?", lastID).Order("id ASC").Limit(discountRepairBatchSize).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			lastID = row.ID
			if strings.TrimSpace(row.DiscountRate) == "" {
				continue
			}
			normalized, changed, invalid := legacyDiscountRate(row.DiscountRate)
			if invalid {
				result.Invalid++
				continue
			}
			if !changed {
				continue
			}
			updates := map[string]any{"discount_rate": normalized}
			if cny, ok := correctedCNY(row.USDTotal, normalized, row.ExchangeRate); ok {
				updates["cny_total"] = cny
			}
			if err := model.DB.Model(row).Updates(updates).Error; err != nil {
				return err
			}
			result.Evaluations++
		}
	}
}

func repairEventDiscountRates(result *DiscountRepairResult) error {
	var lastID int64
	for {
		var rows []*model.BillingAlertEvent
		if err := model.DB.Where("id > ?", lastID).Order("id ASC").Limit(discountRepairBatchSize).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			lastID = row.ID
			if strings.TrimSpace(row.DiscountRate) == "" {
				continue
			}
			normalized, changed, invalid := legacyDiscountRate(row.DiscountRate)
			if invalid {
				result.Invalid++
				continue
			}
			if !changed {
				continue
			}
			updates := map[string]any{"discount_rate": normalized}
			if cny, ok := correctedCNY(row.USDTotal, normalized, row.ExchangeRate); ok {
				updates["cny_total"] = cny
			}
			if err := model.DB.Model(row).Updates(updates).Error; err != nil {
				return err
			}
			result.Events++
		}
	}
}

func repairCycleDiscountRates(result *DiscountRepairResult) error {
	var cycles []*model.BillingCycleSnapshot
	if err := model.DB.Order("id ASC").Find(&cycles).Error; err != nil {
		return err
	}
	for _, cycle := range cycles {
		normalized, changed, invalid := legacyDiscountRate(cycle.DiscountRate)
		if invalid {
			result.Invalid++
			continue
		}
		if !changed {
			continue
		}
		thresholdState, err := repairedCNYThresholdState(cycle)
		if err != nil {
			return err
		}
		if err := model.DB.Model(cycle).Updates(map[string]any{
			"discount_rate":   normalized,
			"threshold_state": thresholdState,
			"updated_at":      common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
		result.Cycles++
	}
	return nil
}

func repairedCNYThresholdState(cycle *model.BillingCycleSnapshot) (string, error) {
	state := map[string]thresholdNotificationState{}
	if err := json.Unmarshal([]byte(cycle.ThresholdState), &state); err != nil {
		return cycle.ThresholdState, nil
	}
	var evaluation model.BillingEvaluationSnapshot
	if err := model.DB.Where("cycle_id = ?", cycle.ID).Order("data_timestamp DESC, id DESC").First(&evaluation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return cycle.ThresholdState, nil
		}
		return "", err
	}
	var thresholds []*model.BillingAlertThreshold
	if err := model.DB.Where("rule_id = ? AND currency = ?", cycle.RuleID, model.BillingCurrencyCNY).Find(&thresholds).Error; err != nil {
		return "", err
	}
	changed := false
	for _, threshold := range thresholds {
		key := strconv.FormatInt(threshold.ID, 10)
		current, exists := state[key]
		if !exists {
			continue
		}
		comparison, err := CompareDecimal(evaluation.CNYTotal, threshold.Amount)
		if err != nil {
			continue
		}
		if comparison < 0 {
			current.LastNotifiedAt = 0
			current.LastAmount = ""
		} else {
			current.LastAmount = evaluation.CNYTotal
		}
		state[key] = current
		changed = true
	}
	if !changed {
		return cycle.ThresholdState, nil
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func legacyDiscountRate(value string) (normalized string, changed bool, invalid bool) {
	parsed, err := parseNonNegativeDecimal(value)
	if err != nil || parsed.Cmp(hundredDecimal) > 0 {
		return "", false, true
	}
	if parsed.Cmp(oneDecimal) <= 0 {
		return value, false, false
	}
	parsed.Quo(parsed, hundredDecimal)
	return formatCompactDecimal(parsed, MoneyScale), true, false
}

func correctedCNY(usd string, discountRate string, exchangeRate string) (string, bool) {
	if usd == "" || exchangeRate == "" {
		return "", false
	}
	value, err := CalculateCNY(usd, discountRate, exchangeRate)
	return value, err == nil
}
