package profile

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

var (
	ErrNotFound      = errors.New("assistant model profile not found")
	ErrInvalidInput  = errors.New("invalid assistant model profile")
	ErrSecretMissing = errors.New("assistant secret encryption is not configured")
)

const modelProfileSecretPurpose = "model-profile-api-key"

type Service struct {
	db     *gorm.DB
	cipher *secrets.Cipher
}

type CreateInput struct {
	Name              string
	Provider          string
	BaseURL           string
	Model             string
	APIKey            string
	TimeoutSeconds    int
	RunTimeoutSeconds int
	MaxOutputTokens   int
	Enabled           bool
	IsPrimary         bool
}

type UpdateInput struct {
	Name              *string
	BaseURL           *string
	Model             *string
	APIKey            *string
	TimeoutSeconds    *int
	RunTimeoutSeconds *int
	MaxOutputTokens   *int
	Enabled           *bool
	IsPrimary         *bool
}

func NewService(db *gorm.DB, cipher *secrets.Cipher) (*Service, error) {
	if db == nil {
		return nil, errors.New("assistant model profile database is nil")
	}
	return &Service{db: db, cipher: cipher}, nil
}

func (s *Service) Create(input CreateInput) (*model.AssistantModelProfile, error) {
	profile := model.AssistantModelProfile{
		Name:              strings.TrimSpace(input.Name),
		Provider:          strings.TrimSpace(input.Provider),
		BaseURL:           strings.TrimSpace(input.BaseURL),
		Model:             strings.TrimSpace(input.Model),
		TimeoutSeconds:    input.TimeoutSeconds,
		RunTimeoutSeconds: input.RunTimeoutSeconds,
		MaxOutputTokens:   input.MaxOutputTokens,
		Enabled:           input.Enabled,
		IsPrimary:         input.IsPrimary,
	}
	applyDefaults(&profile)
	if err := validate(&profile); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.APIKey) != "" && s.cipher == nil {
		return nil, ErrSecretMissing
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if profile.IsPrimary {
			if err := tx.Model(&model.AssistantModelProfile{}).Where("is_primary = ?", true).Update("is_primary", false).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&profile).Error; err != nil {
			return err
		}
		if strings.TrimSpace(input.APIKey) == "" {
			return nil
		}
		ciphertext, version, fingerprint, err := s.cipher.Encrypt(modelProfileSecretPurpose, fmt.Sprint(profile.Id), []byte(strings.TrimSpace(input.APIKey)))
		if err != nil {
			return err
		}
		profile.APIKeyCiphertext = ciphertext
		profile.APIKeyVersion = version
		profile.APIKeyFingerprint = fingerprint
		return tx.Model(&profile).Updates(map[string]any{
			"api_key_ciphertext":  ciphertext,
			"api_key_version":     version,
			"api_key_fingerprint": fingerprint,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *Service) List() ([]model.AssistantModelProfile, error) {
	profiles := make([]model.AssistantModelProfile, 0)
	err := s.db.Order("is_primary DESC, id ASC").Find(&profiles).Error
	return profiles, err
}

func (s *Service) Get(id int64) (*model.AssistantModelProfile, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	var profile model.AssistantModelProfile
	if err := s.db.First(&profile, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &profile, nil
}

func (s *Service) Update(id int64, input UpdateInput) (*model.AssistantModelProfile, error) {
	profile, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		profile.Name = strings.TrimSpace(*input.Name)
	}
	if input.BaseURL != nil {
		profile.BaseURL = strings.TrimSpace(*input.BaseURL)
	}
	if input.Model != nil {
		profile.Model = strings.TrimSpace(*input.Model)
	}
	if input.TimeoutSeconds != nil {
		profile.TimeoutSeconds = *input.TimeoutSeconds
	}
	if input.RunTimeoutSeconds != nil {
		profile.RunTimeoutSeconds = *input.RunTimeoutSeconds
	}
	if input.MaxOutputTokens != nil {
		profile.MaxOutputTokens = *input.MaxOutputTokens
	}
	if input.Enabled != nil {
		profile.Enabled = *input.Enabled
	}
	if input.IsPrimary != nil {
		profile.IsPrimary = *input.IsPrimary
	}
	if err := validate(profile); err != nil {
		return nil, err
	}
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" && s.cipher == nil {
		return nil, ErrSecretMissing
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if profile.IsPrimary {
			if err := tx.Model(&model.AssistantModelProfile{}).Where("id <> ? AND is_primary = ?", profile.Id, true).Update("is_primary", false).Error; err != nil {
				return err
			}
		}
		if input.APIKey != nil {
			secret := strings.TrimSpace(*input.APIKey)
			if secret == "" {
				profile.APIKeyCiphertext = ""
				profile.APIKeyVersion = ""
				profile.APIKeyFingerprint = ""
			} else {
				ciphertext, version, fingerprint, err := s.cipher.Encrypt(modelProfileSecretPurpose, fmt.Sprint(profile.Id), []byte(secret))
				if err != nil {
					return err
				}
				profile.APIKeyCiphertext = ciphertext
				profile.APIKeyVersion = version
				profile.APIKeyFingerprint = fingerprint
			}
		}
		return tx.Save(profile).Error
	})
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *Service) Delete(id int64) error {
	result := s.db.Delete(&model.AssistantModelProfile{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) PrimaryClient() (provider.Client, *model.AssistantModelProfile, error) {
	var profile model.AssistantModelProfile
	err := s.db.Where("enabled = ? AND is_primary = ?", true, true).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return s.clientForProfile(&profile)
}

func (s *Service) Client(id int64) (provider.Client, *model.AssistantModelProfile, error) {
	profile, err := s.Get(id)
	if err != nil {
		return nil, nil, err
	}
	if !profile.Enabled {
		return nil, nil, ErrInvalidInput
	}
	return s.clientForProfile(profile)
}

func (s *Service) clientForProfile(profile *model.AssistantModelProfile) (provider.Client, *model.AssistantModelProfile, error) {
	if profile == nil {
		return nil, nil, ErrNotFound
	}
	apiKey := ""
	if profile.APIKeyCiphertext != "" {
		if s.cipher == nil {
			return nil, nil, ErrSecretMissing
		}
		plaintext, err := s.cipher.Decrypt(modelProfileSecretPurpose, fmt.Sprint(profile.Id), profile.APIKeyVersion, profile.APIKeyCiphertext)
		if err != nil {
			return nil, nil, err
		}
		apiKey = string(plaintext)
	}
	httpClient, err := managedinstance.NewRestrictedHTTPClient(profile.BaseURL, time.Duration(profile.TimeoutSeconds)*time.Second)
	if err != nil {
		return nil, nil, err
	}
	client, err := provider.NewOpenAICompatibleClient(provider.OpenAICompatibleConfig{
		BaseURL:    profile.BaseURL,
		APIKey:     apiKey,
		Model:      profile.Model,
		HTTPClient: httpClient,
	})
	if err != nil {
		return nil, nil, err
	}
	return client, profile, nil
}

func applyDefaults(profile *model.AssistantModelProfile) {
	if profile.Provider == "" {
		profile.Provider = model.AssistantModelProviderOpenAICompatible
	}
	if profile.TimeoutSeconds == 0 {
		profile.TimeoutSeconds = 120
	}
	if profile.RunTimeoutSeconds == 0 {
		profile.RunTimeoutSeconds = 300
	}
	if profile.MaxOutputTokens == 0 {
		profile.MaxOutputTokens = 2048
	}
}

func validate(profile *model.AssistantModelProfile) error {
	if profile == nil || profile.Name == "" || len(profile.Name) > 100 || profile.Model == "" || len(profile.Model) > 160 {
		return ErrInvalidInput
	}
	if profile.Provider != model.AssistantModelProviderOpenAICompatible {
		return fmt.Errorf("%w: unsupported provider", ErrInvalidInput)
	}
	if profile.TimeoutSeconds < 1 || profile.TimeoutSeconds > 120 || profile.RunTimeoutSeconds < 30 || profile.RunTimeoutSeconds > 600 || profile.MaxOutputTokens < 1 || profile.MaxOutputTokens > 32768 {
		return ErrInvalidInput
	}
	_, err := provider.NewOpenAICompatibleClient(provider.OpenAICompatibleConfig{BaseURL: profile.BaseURL, Model: profile.Model})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return nil
}
