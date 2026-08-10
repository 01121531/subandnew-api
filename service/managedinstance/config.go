package managedinstance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidConfigTemplate  = errors.New("invalid managed config template")
	ErrConfigTemplateNotFound = errors.New("managed config template not found")
	ErrConfigTemplateInUse    = errors.New("managed config template is in use")
	ErrConfigBindingNotFound  = errors.New("managed instance config binding not found")
	ErrRemoteConfigInvalid    = errors.New("managed instance remote config is invalid")
	ErrConfigStateConflict    = errors.New("managed instance config changed after preview")
	ErrConfigOperationActive  = errors.New("managed instance config operation is active")
)

type ConfigTemplateInput struct {
	Name          string
	Description   string
	Kind          string
	SchemaVersion int
	Values        json.RawMessage
	ActorID       int
}

type ConfigTemplateView struct {
	*model.ManagedConfigTemplate
	Values map[string]any `json:"values"`
}

type ConfigTemplateList struct {
	Items []*ConfigTemplateView `json:"items"`
}

type ConfigBindingInput struct {
	TemplateID int64
	Mode       string
	ActorID    int
}

type ConfigBindingView struct {
	*model.ManagedInstanceConfigBinding
	Template *ConfigTemplateView `json:"template"`
}

type ConfigDiff struct {
	Key     string `json:"key"`
	Current any    `json:"current"`
	Desired any    `json:"desired"`
}

type ConfigPreview struct {
	Binding      *ConfigBindingView `json:"binding"`
	Observed     map[string]any     `json:"observed"`
	Desired      map[string]any     `json:"desired"`
	Differences  []ConfigDiff       `json:"differences"`
	ObservedHash string             `json:"observed_hash"`
	DesiredHash  string             `json:"desired_hash"`
	Drifted      bool               `json:"drifted"`
	ObservedAt   int64              `json:"observed_at"`
}

func ListConfigSchemas() []ConfigSchema {
	kinds := []string{model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan, model.ManagedInstanceKindSub2API}
	result := make([]ConfigSchema, 0, len(kinds))
	for _, kind := range kinds {
		schema, _ := ConfigSchemaForKind(kind)
		result = append(result, schema)
	}
	return result
}

func CreateConfigTemplate(input ConfigTemplateInput) (*ConfigTemplateView, error) {
	template, values, err := buildConfigTemplate(input)
	if err != nil {
		return nil, err
	}
	if err := model.DB.Create(template).Error; err != nil {
		return nil, err
	}
	return &ConfigTemplateView{ManagedConfigTemplate: template, Values: values}, nil
}

func UpdateConfigTemplate(id int64, input ConfigTemplateInput) (*ConfigTemplateView, error) {
	if id <= 0 {
		return nil, ErrInvalidConfigTemplate
	}
	template, values, err := buildConfigTemplate(input)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"name": template.Name, "description": template.Description, "kind": template.Kind,
		"schema_version": template.SchemaVersion, "values": template.Values,
		"updated_by": input.ActorID, "updated_at": common.GetTimestamp(),
	}
	desiredHash, err := configValuesHash(values)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ManagedConfigTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrConfigTemplateNotFound
			}
			return err
		}
		if current.Kind != template.Kind {
			var count int64
			if err := tx.Model(&model.ManagedInstanceConfigBinding{}).Where("template_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrConfigTemplateInUse
			}
		}
		var instanceIDs []int64
		if err := tx.Model(&model.ManagedInstanceConfigBinding{}).Where("template_id = ?", id).Pluck("instance_id", &instanceIDs).Error; err != nil {
			return err
		}
		if err := ensureNoActiveConfigApply(tx, instanceIDs...); err != nil {
			return err
		}
		if err := tx.Model(&model.ManagedConfigTemplate{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Model(&model.ManagedInstanceConfigBinding{}).Where("template_id = ?", id).Updates(map[string]any{
			"desired_hash": desiredHash, "drift_status": model.ManagedConfigDriftUnknown,
			"last_error_code": "", "updated_by": input.ActorID, "updated_at": common.GetTimestamp(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return GetConfigTemplate(id)
}

func GetConfigTemplate(id int64) (*ConfigTemplateView, error) {
	var template model.ManagedConfigTemplate
	if id <= 0 {
		return nil, ErrInvalidConfigTemplate
	}
	if err := model.DB.First(&template, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConfigTemplateNotFound
		}
		return nil, err
	}
	return configTemplateView(&template)
}

func ListConfigTemplates(kind string) (*ConfigTemplateList, error) {
	query := model.DB.Order("name asc")
	if strings.TrimSpace(kind) != "" {
		query = query.Where("kind = ?", strings.TrimSpace(kind))
	}
	var templates []*model.ManagedConfigTemplate
	if err := query.Find(&templates).Error; err != nil {
		return nil, err
	}
	result := make([]*ConfigTemplateView, 0, len(templates))
	for _, template := range templates {
		view, err := configTemplateView(template)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return &ConfigTemplateList{Items: result}, nil
}

func DeleteConfigTemplate(id int64) error {
	if id <= 0 {
		return ErrInvalidConfigTemplate
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var template model.ManagedConfigTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&template, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrConfigTemplateNotFound
			}
			return err
		}
		var count int64
		if err := tx.Model(&model.ManagedInstanceConfigBinding{}).Where("template_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrConfigTemplateInUse
		}
		result := tx.Delete(&model.ManagedConfigTemplate{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrConfigTemplateNotFound
		}
		return nil
	})
}

func SetConfigBinding(instanceID int64, input ConfigBindingInput) (*ConfigBindingView, error) {
	if instanceID <= 0 || input.TemplateID <= 0 || input.ActorID <= 0 || !validConfigMode(input.Mode) {
		return nil, ErrInvalidConfigTemplate
	}
	instance, err := getOperationInstance(instanceID)
	if err != nil {
		return nil, err
	}
	template, err := GetConfigTemplate(input.TemplateID)
	if err != nil {
		return nil, err
	}
	if template.Kind != instance.Kind {
		return nil, fmt.Errorf("%w: template kind does not match instance", ErrInvalidConfigTemplate)
	}
	desiredHash, _ := configValuesHash(template.Values)
	binding := &model.ManagedInstanceConfigBinding{
		InstanceId: instanceID, TemplateId: input.TemplateID, Mode: input.Mode,
		DriftStatus: model.ManagedConfigDriftUnknown, DesiredHash: desiredHash,
		CreatedBy: input.ActorID, UpdatedBy: input.ActorID,
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureNoActiveConfigApply(tx, instanceID); err != nil {
			return err
		}
		var currentInstance model.ManagedInstance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&currentInstance, instanceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInstanceNotFound
			}
			return err
		}
		var currentTemplate model.ManagedConfigTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&currentTemplate, input.TemplateID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrConfigTemplateNotFound
			}
			return err
		}
		if currentTemplate.Kind != currentInstance.Kind || currentTemplate.SchemaVersion != template.SchemaVersion || currentTemplate.Values != template.ManagedConfigTemplate.Values {
			return ErrConfigStateConflict
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "instance_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"template_id": input.TemplateID, "mode": input.Mode,
				"drift_status": model.ManagedConfigDriftUnknown, "desired_hash": desiredHash,
				"last_observed_hash": "", "last_error_code": "", "last_checked_at": 0,
				"updated_by": input.ActorID, "updated_at": common.GetTimestamp(),
			}),
		}).Create(binding).Error; err != nil {
			return err
		}
		return writeAudit(tx, instanceID, input.ActorID, "config_binding_update", map[string]any{
			"template_id": input.TemplateID, "mode": input.Mode,
		})
	})
	if err != nil {
		return nil, err
	}
	return GetConfigBinding(instanceID)
}

func GetConfigBinding(instanceID int64) (*ConfigBindingView, error) {
	var binding model.ManagedInstanceConfigBinding
	if err := model.DB.Where("instance_id = ?", instanceID).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConfigBindingNotFound
		}
		return nil, err
	}
	template, err := GetConfigTemplate(binding.TemplateId)
	if err != nil {
		return nil, err
	}
	return &ConfigBindingView{ManagedInstanceConfigBinding: &binding, Template: template}, nil
}

func RefreshConfigPreview(ctx context.Context, instanceID int64, actorID int) (*ConfigPreview, error) {
	binding, err := GetConfigBinding(instanceID)
	if err != nil {
		return nil, err
	}
	instance, err := getOperationInstance(instanceID)
	if err != nil {
		return nil, err
	}
	if binding.Template.Kind != instance.Kind {
		return nil, ErrInvalidConfigTemplate
	}
	observed, err := readRemoteConfig(ctx, instance, binding.Template.Values)
	if err != nil {
		_ = updateConfigBindingObservation(binding, "", model.ManagedConfigDriftFailed, managedInstanceOperationErrorCode(err))
		return nil, err
	}
	preview := buildConfigPreview(binding, observed)
	status := model.ManagedConfigDriftInSync
	if preview.Drifted {
		status = model.ManagedConfigDriftDrifted
	}
	if err := updateConfigBindingObservation(binding, preview.ObservedHash, status, ""); err != nil {
		return nil, err
	}
	if actorID > 0 {
		_ = model.DB.Transaction(func(tx *gorm.DB) error {
			return writeAudit(tx, instanceID, actorID, "config_drift_check", map[string]any{
				"template_id": binding.TemplateId, "drifted": preview.Drifted, "difference_count": len(preview.Differences),
			})
		})
	}
	refreshed, err := GetConfigBinding(instanceID)
	if err == nil {
		preview.Binding = refreshed
	}
	return preview, err
}

func buildConfigTemplate(input ConfigTemplateInput) (*model.ManagedConfigTemplate, map[string]any, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.ActorID <= 0 || input.Name == "" || len(input.Name) > 128 || len(input.Description) > 512 {
		return nil, nil, ErrInvalidConfigTemplate
	}
	values, err := validateConfigValues(input.Kind, input.SchemaVersion, input.Values)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, nil, err
	}
	return &model.ManagedConfigTemplate{
		Name: input.Name, Description: input.Description, Kind: input.Kind,
		SchemaVersion: input.SchemaVersion, Values: string(encoded), CreatedBy: input.ActorID, UpdatedBy: input.ActorID,
	}, values, nil
}

func configTemplateView(template *model.ManagedConfigTemplate) (*ConfigTemplateView, error) {
	var values map[string]any
	if err := json.Unmarshal([]byte(template.Values), &values); err != nil {
		return nil, ErrInvalidConfigTemplate
	}
	return &ConfigTemplateView{ManagedConfigTemplate: template, Values: values}, nil
}

func readRemoteConfig(ctx context.Context, instance *model.ManagedInstance, desired map[string]any) (map[string]any, error) {
	credential, err := loadCredential(instance.Id)
	if err != nil {
		return nil, err
	}
	policy, err := ConnectorPolicyFromEnvironment()
	if err != nil {
		return nil, err
	}
	connector, err := NewConnector(instance, policy)
	if err != nil {
		return nil, err
	}
	var headers http.Header
	switch instance.Kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan:
		headers, err = newAPIAuthHeaders(ctx, connector, instance.Kind, credential)
	case model.ManagedInstanceKindSub2API:
		headers, err = sub2APIAuthHeaders(ctx, connector, credential)
	default:
		return nil, ErrUnsupportedCapability
	}
	if err != nil {
		return nil, err
	}
	return readRemoteConfigWithConnector(ctx, connector, instance.Kind, headers, desired)
}

func readRemoteConfigWithConnector(ctx context.Context, connector *Connector, kind string, headers http.Header, desired map[string]any) (map[string]any, error) {
	var remote map[string]any
	switch kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan:
		response, err := connector.DoJSON(ctx, http.MethodGet, "/api/option/", headers, nil)
		if err != nil {
			return nil, err
		}
		if err := requireNewAPISuccess(response); err != nil {
			return nil, err
		}
		var envelope struct {
			Data []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body, &envelope); err != nil {
			return nil, ErrRemoteConfigInvalid
		}
		remote = make(map[string]any, len(envelope.Data))
		for _, option := range envelope.Data {
			remote[option.Key] = option.Value
		}
	case model.ManagedInstanceKindSub2API:
		response, err := connector.DoJSON(ctx, http.MethodGet, "/api/v1/admin/settings", headers, nil)
		if err != nil {
			return nil, err
		}
		if err := requireHTTPStatus(response); err != nil {
			return nil, err
		}
		decoded, decodeErr := decodeSub2Settings(response.Body)
		if decodeErr != nil {
			return nil, decodeErr
		}
		remote = decoded
	default:
		return nil, ErrUnsupportedCapability
	}
	return normalizeRemoteConfig(kind, desired, remote)
}

func decodeSub2Settings(body []byte) (map[string]any, error) {
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, ErrRemoteConfigInvalid
	}
	code, codeExists := envelope["code"]
	data, dataExists := envelope["data"].(map[string]any)
	if !codeExists || !dataExists || !sub2SuccessCode(code) {
		return nil, ErrRemoteConfigInvalid
	}
	return data, nil
}

func buildConfigPreview(binding *ConfigBindingView, observed map[string]any) *ConfigPreview {
	desired := binding.Template.Values
	differences := make([]ConfigDiff, 0)
	for key, desiredValue := range desired {
		if !configValuesEqual(observed[key], desiredValue) {
			differences = append(differences, ConfigDiff{Key: key, Current: observed[key], Desired: desiredValue})
		}
	}
	observedHash, _ := configValuesHash(observed)
	desiredHash, _ := configValuesHash(desired)
	return &ConfigPreview{
		Binding: binding, Observed: observed, Desired: desired, Differences: differences,
		ObservedHash: observedHash, DesiredHash: desiredHash, Drifted: len(differences) > 0,
		ObservedAt: common.GetTimestamp(),
	}
}

func updateConfigBindingObservation(binding *ConfigBindingView, observedHash string, status string, errorCode string) error {
	if binding == nil || binding.Template == nil {
		return ErrConfigStateConflict
	}
	result := model.DB.Model(&model.ManagedInstanceConfigBinding{}).
		Where("instance_id = ? AND template_id = ? AND desired_hash = ?", binding.InstanceId, binding.TemplateId, binding.DesiredHash).
		Updates(map[string]any{
			"last_observed_hash": observedHash, "drift_status": status,
			"last_error_code": errorCode, "last_checked_at": common.GetTimestamp(), "updated_at": common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		var current model.ManagedInstanceConfigBinding
		if err := model.DB.Where("instance_id = ?", binding.InstanceId).First(&current).Error; err != nil {
			return ErrConfigStateConflict
		}
		if current.TemplateId != binding.TemplateId || current.DesiredHash != binding.DesiredHash {
			return ErrConfigStateConflict
		}
	}
	return nil
}

func configValuesHash(values map[string]any) (string, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func configValuesEqual(left any, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func validConfigMode(mode string) bool {
	switch mode {
	case model.ManagedConfigModeDisabled, model.ManagedConfigModeAudit, model.ManagedConfigModeEnforce:
		return true
	default:
		return false
	}
}

func ensureNoActiveConfigApply(tx *gorm.DB, instanceIDs ...int64) error {
	if len(instanceIDs) == 0 {
		return nil
	}
	if !tx.Migrator().HasTable(&model.ManagedInstanceOperation{}) {
		return nil
	}
	var count int64
	if err := tx.Model(&model.ManagedInstanceOperation{}).
		Where("instance_id IN ? AND action = ? AND status IN ?", instanceIDs, model.ManagedInstanceActionApplyConfig, []string{
			model.ManagedInstanceOperationStatusQueued, model.ManagedInstanceOperationStatusRunning,
		}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrConfigOperationActive
	}
	return nil
}
