package managedinstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const (
	AccountFilterMatchAll = "all"
	AccountFilterMatchAny = "any"
	AccountFilterValueAny = "any"
	AccountFilterValueAll = "all"
)

var (
	ErrInvalidAccountFilterTemplate  = errors.New("invalid managed account filter template")
	ErrAccountFilterTemplateMissing  = errors.New("managed account filter template not found")
	ErrAccountFilterTemplateConflict = errors.New("managed account filter template name already exists")
)

type AccountFilterRule struct {
	Field     string   `json:"field"`
	Operator  string   `json:"operator"`
	Values    []string `json:"values,omitempty"`
	ValueMode string   `json:"value_mode"`
}

type AccountFilterTemplateInput struct {
	Name      string              `json:"name"`
	MatchMode string              `json:"match_mode"`
	Rules     []AccountFilterRule `json:"rules"`
}

type AccountFilterTemplateView struct {
	Id        int64               `json:"id"`
	Name      string              `json:"name"`
	MatchMode string              `json:"match_mode"`
	Rules     []AccountFilterRule `json:"rules"`
	CreatedAt int64               `json:"created_at"`
	UpdatedAt int64               `json:"updated_at"`
}

var accountFilterFieldType = map[string]string{
	"name": "text", "email": "text", "account_id": "text", "note": "text", "ownership": "text",
	"instance": "category", "platform": "category", "type": "category", "group": "category",
	"status": "category", "source": "category", "available": "category",
}

var accountTextFilterOperators = map[string]bool{
	"contains": true, "not_contains": true, "is_empty": true, "is_not_empty": true,
}

var accountCategoryFilterOperators = map[string]bool{
	"is": true, "is_not": true, "is_empty": true, "is_not_empty": true,
}

func CreateAccountFilterTemplate(actorID int, input AccountFilterTemplateInput) (*AccountFilterTemplateView, error) {
	prepared, encoded, err := prepareAccountFilterTemplate(actorID, input)
	if err != nil {
		return nil, err
	}
	if accountFilterTemplateNameExists(actorID, prepared.Name, 0) {
		return nil, ErrAccountFilterTemplateConflict
	}
	template := &model.ManagedAccountFilterTemplate{
		ActorID: actorID, Name: prepared.Name, MatchMode: prepared.MatchMode, Rules: string(encoded),
	}
	if err := model.DB.Create(template).Error; err != nil {
		if accountFilterTemplateNameExists(actorID, prepared.Name, 0) {
			return nil, ErrAccountFilterTemplateConflict
		}
		return nil, err
	}
	return accountFilterTemplateView(template)
}

func ListAccountFilterTemplates(actorID int) ([]*AccountFilterTemplateView, error) {
	if actorID <= 0 {
		return nil, ErrInvalidAccountFilterTemplate
	}
	var templates []*model.ManagedAccountFilterTemplate
	if err := model.DB.Where("actor_id = ?", actorID).Order("updated_at DESC, id DESC").Find(&templates).Error; err != nil {
		return nil, err
	}
	views := make([]*AccountFilterTemplateView, 0, len(templates))
	for _, template := range templates {
		view, err := accountFilterTemplateView(template)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func UpdateAccountFilterTemplate(id int64, actorID int, input AccountFilterTemplateInput) (*AccountFilterTemplateView, error) {
	if id <= 0 {
		return nil, ErrInvalidAccountFilterTemplate
	}
	prepared, encoded, err := prepareAccountFilterTemplate(actorID, input)
	if err != nil {
		return nil, err
	}
	var template model.ManagedAccountFilterTemplate
	if err := model.DB.Where("id = ? AND actor_id = ?", id, actorID).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAccountFilterTemplateMissing
		}
		return nil, err
	}
	if accountFilterTemplateNameExists(actorID, prepared.Name, id) {
		return nil, ErrAccountFilterTemplateConflict
	}
	template.Name = prepared.Name
	template.MatchMode = prepared.MatchMode
	template.Rules = string(encoded)
	if err := model.DB.Save(&template).Error; err != nil {
		if accountFilterTemplateNameExists(actorID, prepared.Name, id) {
			return nil, ErrAccountFilterTemplateConflict
		}
		return nil, err
	}
	return accountFilterTemplateView(&template)
}

func DeleteAccountFilterTemplate(id int64, actorID int) error {
	if id <= 0 || actorID <= 0 {
		return ErrInvalidAccountFilterTemplate
	}
	result := model.DB.Where("id = ? AND actor_id = ?", id, actorID).Delete(&model.ManagedAccountFilterTemplate{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrAccountFilterTemplateMissing
	}
	return nil
}

func prepareAccountFilterTemplate(actorID int, input AccountFilterTemplateInput) (AccountFilterTemplateInput, []byte, error) {
	input.Name = strings.TrimSpace(input.Name)
	if actorID <= 0 || input.Name == "" || utf8.RuneCountInString(input.Name) > 64 {
		return input, nil, ErrInvalidAccountFilterTemplate
	}
	matchMode, rules, err := NormalizeAccountFilter(input.MatchMode, input.Rules, true)
	if err != nil {
		return input, nil, err
	}
	input.MatchMode = matchMode
	input.Rules = rules
	encoded, err := json.Marshal(input.Rules)
	if err != nil {
		return input, nil, err
	}
	return input, encoded, nil
}

// NormalizeAccountFilter validates and normalizes the shared account filter
// representation used by templates, assistant queries, and external feeds.
func NormalizeAccountFilter(matchMode string, rules []AccountFilterRule, requireRules bool) (string, []AccountFilterRule, error) {
	matchMode = strings.TrimSpace(matchMode)
	if matchMode == "" {
		matchMode = AccountFilterMatchAll
	}
	if (matchMode != AccountFilterMatchAll && matchMode != AccountFilterMatchAny) || len(rules) > 20 || (requireRules && len(rules) == 0) {
		return "", nil, ErrInvalidAccountFilterTemplate
	}
	normalizedRules := append([]AccountFilterRule(nil), rules...)
	for index := range normalizedRules {
		rule := &normalizedRules[index]
		rule.Field = strings.TrimSpace(rule.Field)
		rule.Operator = strings.TrimSpace(rule.Operator)
		rule.ValueMode = strings.TrimSpace(rule.ValueMode)
		fieldType, ok := accountFilterFieldType[rule.Field]
		operators := accountTextFilterOperators
		if fieldType == "category" {
			operators = accountCategoryFilterOperators
		}
		if !ok || !operators[rule.Operator] || (rule.ValueMode != AccountFilterValueAny && rule.ValueMode != AccountFilterValueAll) {
			return "", nil, fmt.Errorf("%w: rule %d", ErrInvalidAccountFilterTemplate, index+1)
		}
		emptyOperator := rule.Operator == "is_empty" || rule.Operator == "is_not_empty"
		if emptyOperator {
			rule.Values = nil
			continue
		}
		if len(rule.Values) == 0 || len(rule.Values) > 50 {
			return "", nil, fmt.Errorf("%w: rule %d values", ErrInvalidAccountFilterTemplate, index+1)
		}
		seen := make(map[string]struct{}, len(rule.Values))
		normalized := make([]string, 0, len(rule.Values))
		for _, raw := range rule.Values {
			value := strings.TrimSpace(raw)
			key := strings.ToLower(value)
			if value == "" || utf8.RuneCountInString(value) > 200 {
				return "", nil, fmt.Errorf("%w: rule %d value", ErrInvalidAccountFilterTemplate, index+1)
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			normalized = append(normalized, value)
		}
		if len(normalized) == 0 {
			return "", nil, fmt.Errorf("%w: rule %d values", ErrInvalidAccountFilterTemplate, index+1)
		}
		rule.Values = normalized
	}
	return matchMode, normalizedRules, nil
}

func accountFilterTemplateView(template *model.ManagedAccountFilterTemplate) (*AccountFilterTemplateView, error) {
	var rules []AccountFilterRule
	if err := json.Unmarshal([]byte(template.Rules), &rules); err != nil {
		return nil, err
	}
	return &AccountFilterTemplateView{
		Id: template.Id, Name: template.Name, MatchMode: template.MatchMode, Rules: rules,
		CreatedAt: template.CreatedAt, UpdatedAt: template.UpdatedAt,
	}, nil
}

func accountFilterTemplateNameExists(actorID int, name string, excludeID int64) bool {
	query := model.DB.Model(&model.ManagedAccountFilterTemplate{}).Where("actor_id = ? AND name = ?", actorID, name)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	return query.Count(&count).Error == nil && count > 0
}
