package managedinstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidInstance           = errors.New("invalid managed instance")
	ErrInstanceNotFound          = errors.New("managed instance not found")
	ErrInstanceAlreadyExists     = errors.New("managed instance already exists")
	ErrConnectionChangeForbidden = errors.New("managed instance connection change requires secret rotation permission")
	ErrWriteModeForbidden        = errors.New("managed instance write mode requires root permission")
)

type CredentialInput struct {
	AuthType  string
	Secret    string
	UserID    string
	ExpiresAt int64
}

type CreateInput struct {
	Name                  string
	Kind                  string
	BaseURL               string
	Environment           string
	Labels                map[string]string
	ManagementMode        string
	TLSVerify             bool
	RequestTimeoutSeconds int
	CheckIntervalSeconds  int
	Credential            *CredentialInput
	Preflight             *ProbeResult
	ActorID               int
	AllowWriteMode        bool
}

type UpdateInput struct {
	Name                  string
	Kind                  string
	BaseURL               string
	Environment           string
	Labels                map[string]string
	ManagementMode        string
	TLSVerify             bool
	RequestTimeoutSeconds int
	CheckIntervalSeconds  int
	ActorID               int
	AllowConnectionChange bool
	AllowWriteMode        bool
}

type ListFilter struct {
	Kind             string
	Environment      string
	Status           string
	Search           string
	Page             int
	PageSize         int
	SearchConnection bool
}

type InstanceView struct {
	*model.ManagedInstance
	Labels       map[string]string `json:"labels"`
	Capabilities []string          `json:"capabilities"`
	Credential   *CredentialView   `json:"credential,omitempty"`
}

type CredentialView struct {
	AuthType       string `json:"auth_type"`
	Fingerprint    string `json:"fingerprint"`
	ExpiresAt      int64  `json:"expires_at"`
	LastVerifiedAt int64  `json:"last_verified_at"`
	RotatedAt      int64  `json:"rotated_at"`
}

type ListResult struct {
	Items    []*InstanceView `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type AuditListResult struct {
	Items    []*model.ManagedInstanceAudit `json:"items"`
	Total    int64                         `json:"total"`
	Page     int                           `json:"page"`
	PageSize int                           `json:"page_size"`
}

func RedactConnectionDetails(view *InstanceView) *InstanceView {
	if view == nil || view.ManagedInstance == nil {
		return view
	}
	instance := *view.ManagedInstance
	instance.BaseURL = ""
	instance.Labels = ""
	instance.TLSVerify = false
	instance.RequestTimeoutSeconds = 0
	instance.CheckIntervalSeconds = 0
	instance.CreatedBy = 0
	instance.UpdatedBy = 0
	return &InstanceView{
		ManagedInstance: &instance,
		Labels:          map[string]string{},
		Capabilities:    append([]string(nil), view.Capabilities...),
	}
}

func Create(input CreateInput) (*InstanceView, error) {
	instance, err := buildInstance(input.Name, input.Kind, input.BaseURL, input.Environment, input.Labels, input.ManagementMode, input.TLSVerify, input.RequestTimeoutSeconds, input.CheckIntervalSeconds, input.ActorID)
	if err != nil {
		return nil, err
	}
	if instance.ManagementMode != model.ManagedInstanceModeObserve && !input.AllowWriteMode {
		return nil, ErrWriteModeForbidden
	}
	if input.Preflight != nil {
		if input.Preflight.Status != model.ManagedInstanceStatusHealthy || !validKind(input.Preflight.Kind) {
			return nil, ErrInvalidInstance
		}
		capabilities, err := json.Marshal(input.Preflight.Capabilities)
		if err != nil {
			return nil, err
		}
		instance.Kind = input.Preflight.Kind
		instance.Version = input.Preflight.Version
		instance.Capabilities = string(capabilities)
		instance.Status = model.ManagedInstanceStatusHealthy
		instance.LastSeenAt = input.Preflight.CheckedAt
		instance.LastCheckedAt = input.Preflight.CheckedAt
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var duplicateCount int64
		if err := tx.Model(&model.ManagedInstance{}).
			Where("name = ? OR base_url = ?", instance.Name, instance.BaseURL).
			Count(&duplicateCount).Error; err != nil {
			return err
		}
		if duplicateCount > 0 {
			return ErrInstanceAlreadyExists
		}
		if err := tx.Create(instance).Error; err != nil {
			return err
		}
		if input.Credential != nil {
			credential, err := buildCredential(instance.Id, *input.Credential, input.ActorID)
			if err != nil {
				return err
			}
			if err := tx.Create(credential).Error; err != nil {
				return err
			}
			if input.Preflight != nil {
				if err := tx.Model(credential).Update("last_verified_at", input.Preflight.CheckedAt).Error; err != nil {
					return err
				}
			}
		}
		return writeAudit(tx, instance.Id, input.ActorID, "create", map[string]any{"name": instance.Name, "kind": instance.Kind})
	})
	if err != nil {
		return nil, err
	}
	return Get(instance.Id)
}

func Get(id int64) (*InstanceView, error) {
	if id <= 0 {
		return nil, ErrInvalidInstance
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}
	var credential model.ManagedInstanceCredential
	credentialErr := model.DB.Where("instance_id = ?", id).First(&credential).Error
	if credentialErr != nil && !errors.Is(credentialErr, gorm.ErrRecordNotFound) {
		return nil, credentialErr
	}
	return newInstanceView(&instance, credentialOrNil(&credential, credentialErr)), nil
}

func List(filter ListFilter) (*ListResult, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	query := model.DB.Model(&model.ManagedInstance{})
	if filter.Kind != "" {
		query = query.Where("kind = ?", filter.Kind)
	}
	if filter.Environment != "" {
		query = query.Where("environment = ?", filter.Environment)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		if filter.SearchConnection {
			query = query.Where("name LIKE ? OR base_url LIKE ?", "%"+search+"%", "%"+search+"%")
		} else {
			query = query.Where("name LIKE ?", "%"+search+"%")
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var instances []*model.ManagedInstance
	if err := query.Order("id desc").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&instances).Error; err != nil {
		return nil, err
	}
	credentials := make(map[int64]*model.ManagedInstanceCredential)
	if len(instances) > 0 {
		ids := make([]int64, 0, len(instances))
		for _, instance := range instances {
			ids = append(ids, instance.Id)
		}
		var rows []*model.ManagedInstanceCredential
		if err := model.DB.Where("instance_id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, credential := range rows {
			credentials[credential.InstanceId] = credential
		}
	}
	views := make([]*InstanceView, 0, len(instances))
	for _, instance := range instances {
		views = append(views, newInstanceView(instance, credentials[instance.Id]))
	}
	return &ListResult{Items: views, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func ListAudits(instanceID int64, page int, pageSize int) (*AuditListResult, error) {
	if instanceID <= 0 {
		return nil, ErrInvalidInstance
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := model.DB.Model(&model.ManagedInstanceAudit{}).Where("instance_id = ?", instanceID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var audits []*model.ManagedInstanceAudit
	if err := query.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&audits).Error; err != nil {
		return nil, err
	}
	return &AuditListResult{Items: audits, Total: total, Page: page, PageSize: pageSize}, nil
}

func Update(id int64, input UpdateInput) (*InstanceView, error) {
	if id <= 0 {
		return nil, ErrInvalidInstance
	}
	instance, err := buildInstance(input.Name, input.Kind, input.BaseURL, input.Environment, input.Labels, input.ManagementMode, input.TLSVerify, input.RequestTimeoutSeconds, input.CheckIntervalSeconds, input.ActorID)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"name": instance.Name, "kind": instance.Kind, "base_url": instance.BaseURL,
		"environment": instance.Environment, "labels": instance.Labels, "management_mode": instance.ManagementMode,
		"tls_verify": instance.TLSVerify, "request_timeout_seconds": instance.RequestTimeoutSeconds,
		"check_interval_seconds": instance.CheckIntervalSeconds, "updated_by": input.ActorID,
		"updated_at": common.GetTimestamp(),
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ManagedInstance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInstanceNotFound
			}
			return err
		}
		connectionChanged := current.BaseURL != instance.BaseURL || current.Kind != instance.Kind || current.TLSVerify != instance.TLSVerify
		if connectionChanged && !input.AllowConnectionChange {
			return ErrConnectionChangeForbidden
		}
		if current.ManagementMode != instance.ManagementMode && instance.ManagementMode != model.ManagedInstanceModeObserve && !input.AllowWriteMode {
			return ErrWriteModeForbidden
		}
		if connectionChanged || current.ManagementMode != instance.ManagementMode {
			if err := ensureNoActiveConfigApply(tx, id); err != nil {
				return err
			}
		}
		result := tx.Model(&model.ManagedInstance{}).Where("id = ?", id).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrInstanceNotFound
		}
		return writeAudit(tx, id, input.ActorID, "update", map[string]any{
			"name": instance.Name, "kind_before": current.Kind, "kind_after": instance.Kind,
			"base_url_changed":  current.BaseURL != instance.BaseURL,
			"tls_verify_before": current.TLSVerify, "tls_verify_after": instance.TLSVerify,
		})
	})
	if err != nil {
		return nil, err
	}
	return Get(id)
}

func RotateCredential(instanceID int64, input CredentialInput, actorID int) (*CredentialView, error) {
	if instanceID <= 0 {
		return nil, ErrInvalidInstance
	}
	credential, err := buildCredential(instanceID, input, actorID)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := model.DB.Model(&model.ManagedInstance{}).Where("id = ?", instanceID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrInstanceNotFound
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureNoActiveConfigApply(tx, instanceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "instance_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"auth_type", "ciphertext", "key_version", "fingerprint", "expires_at", "last_verified_at", "rotated_by", "rotated_at", "updated_at",
			}),
		}).Create(credential).Error; err != nil {
			return err
		}
		return writeAudit(tx, instanceID, actorID, "credential_rotate", map[string]any{"auth_type": credential.AuthType, "key_version": credential.KeyVersion})
	})
	if err != nil {
		return nil, err
	}
	return newCredentialView(credential), nil
}

func Delete(id int64, actorID int) error {
	if id <= 0 {
		return ErrInvalidInstance
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureNoActiveConfigApply(tx, id); err != nil {
			return err
		}
		if err := tx.Where("instance_id = ?", id).Delete(&model.ManagedInstanceCredential{}).Error; err != nil {
			return err
		}
		if err := tx.Where("instance_id = ?", id).Delete(&model.ManagedInstanceSnapshot{}).Error; err != nil {
			return err
		}
		if err := tx.Where("instance_id = ?", id).Delete(&model.ManagedInstanceAlert{}).Error; err != nil {
			return err
		}
		if err := tx.Where("instance_id = ?", id).Delete(&model.ManagedInstanceConfigBinding{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.ManagedInstance{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrInstanceNotFound
		}
		return writeAudit(tx, id, actorID, "delete", nil)
	})
}

func buildInstance(name string, kind string, baseURL string, environment string, labels map[string]string, managementMode string, tlsVerify bool, requestTimeout int, checkInterval int, actorID int) (*model.ManagedInstance, error) {
	name = strings.TrimSpace(name)
	kind = strings.TrimSpace(kind)
	if name == "" || !validKind(kind) {
		return nil, ErrInvalidInstance
	}
	normalizedURL, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	environment = strings.TrimSpace(environment)
	if environment == "" {
		environment = "production"
	}
	managementMode = strings.TrimSpace(managementMode)
	if managementMode == "" {
		managementMode = model.ManagedInstanceModeObserve
	}
	if !validEnvironment(environment) || !validManagementMode(managementMode) {
		return nil, ErrInvalidInstance
	}
	if requestTimeout == 0 {
		requestTimeout = 10
	}
	if checkInterval == 0 {
		checkInterval = 60
	}
	if requestTimeout < 1 || requestTimeout > 120 || checkInterval < 10 || checkInterval > 86400 {
		return nil, ErrInvalidInstance
	}
	parsedURL, err := url.Parse(normalizedURL)
	if err != nil {
		return nil, ErrInvalidInstance
	}
	if environment == "production" && (parsedURL.Scheme != "https" || !tlsVerify) {
		return nil, fmt.Errorf("%w: production instances require HTTPS with TLS verification", ErrInvalidInstance)
	}
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return nil, fmt.Errorf("%w: labels", ErrInvalidInstance)
	}
	return &model.ManagedInstance{
		Name: name, Kind: kind, BaseURL: normalizedURL, Environment: environment, Labels: string(labelsJSON),
		ManagementMode: managementMode, Status: model.ManagedInstanceStatusUnknown, TLSVerify: tlsVerify,
		RequestTimeoutSeconds: requestTimeout, CheckIntervalSeconds: checkInterval, CreatedBy: actorID, UpdatedBy: actorID,
	}, nil
}

func buildCredential(instanceID int64, input CredentialInput, actorID int) (*model.ManagedInstanceCredential, error) {
	input.AuthType = strings.TrimSpace(input.AuthType)
	if !validAuthType(input.AuthType) {
		return nil, fmt.Errorf("%w: unsupported credential type", ErrInvalidInstance)
	}
	if strings.TrimSpace(input.Secret) == "" {
		return nil, fmt.Errorf("%w: credential secret is required", ErrInvalidInstance)
	}
	cipher, err := NewCredentialCipherFromEnvironment()
	if err != nil {
		return nil, err
	}
	ciphertext, keyVersion, fingerprint, err := cipher.Encrypt(instanceID, input.AuthType, CredentialPayload{Secret: input.Secret, UserID: strings.TrimSpace(input.UserID)})
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	return &model.ManagedInstanceCredential{
		InstanceId: instanceID, AuthType: input.AuthType, Ciphertext: ciphertext, KeyVersion: keyVersion,
		Fingerprint: fingerprint, ExpiresAt: input.ExpiresAt, RotatedBy: actorID, RotatedAt: now,
	}, nil
}

func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: base URL must use http or https", ErrInvalidInstance)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: base URL cannot include user info, query, or fragment", ErrInvalidInstance)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func validKind(kind string) bool {
	switch kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan, model.ManagedInstanceKindSub2API, model.ManagedInstanceKindGeneric:
		return true
	default:
		return false
	}
}

func validManagementMode(mode string) bool {
	switch mode {
	case model.ManagedInstanceModeObserve, model.ManagedInstanceModeOperate, model.ManagedInstanceModeEnforce:
		return true
	default:
		return false
	}
}

func validEnvironment(environment string) bool {
	switch environment {
	case "production", "staging", "development":
		return true
	default:
		return false
	}
}

func validAuthType(authType string) bool {
	switch authType {
	case "bearer_pat", "admin_token", "legacy_access_token", "account_password":
		return true
	default:
		return false
	}
}

func newInstanceView(instance *model.ManagedInstance, credential *model.ManagedInstanceCredential) *InstanceView {
	labels := map[string]string{}
	capabilities := []string{}
	_ = json.Unmarshal([]byte(instance.Labels), &labels)
	_ = json.Unmarshal([]byte(instance.Capabilities), &capabilities)
	return &InstanceView{ManagedInstance: instance, Labels: labels, Capabilities: capabilities, Credential: newCredentialView(credential)}
}

func newCredentialView(credential *model.ManagedInstanceCredential) *CredentialView {
	if credential == nil {
		return nil
	}
	fingerprint := credential.Fingerprint
	if len(fingerprint) > 8 {
		fingerprint = fingerprint[len(fingerprint)-8:]
	}
	return &CredentialView{
		AuthType: credential.AuthType, Fingerprint: fingerprint, ExpiresAt: credential.ExpiresAt,
		LastVerifiedAt: credential.LastVerifiedAt, RotatedAt: credential.RotatedAt,
	}
}

func credentialOrNil(credential *model.ManagedInstanceCredential, err error) *model.ManagedInstanceCredential {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return credential
}

func writeAudit(tx *gorm.DB, instanceID int64, actorID int, action string, details map[string]any) error {
	return writeAuditOutcome(tx, instanceID, actorID, action, "succeeded", details)
}

func writeAuditOutcome(tx *gorm.DB, instanceID int64, actorID int, action string, outcome string, details map[string]any) error {
	detailsJSON := "{}"
	if details != nil {
		encoded, err := json.Marshal(details)
		if err != nil {
			return err
		}
		detailsJSON = string(encoded)
	}
	return tx.Create(&model.ManagedInstanceAudit{
		InstanceId: instanceID,
		ActorId:    actorID,
		Action:     action,
		Outcome:    outcome,
		Details:    detailsJSON,
	}).Error
}
