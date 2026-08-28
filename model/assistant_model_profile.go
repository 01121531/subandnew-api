package model

const (
	AssistantModelProviderOpenAICompatible = "openai_compatible"
)

// AssistantModelProfile describes one server-managed LLM endpoint. API keys
// are encrypted by service/assistant/secrets and never serialized to clients.
type AssistantModelProfile struct {
	Id                int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	Name              string `json:"name" gorm:"type:varchar(100);not null;uniqueIndex"`
	Provider          string `json:"provider" gorm:"type:varchar(40);not null;index"`
	BaseURL           string `json:"base_url" gorm:"type:varchar(500);not null"`
	Model             string `json:"model" gorm:"type:varchar(160);not null"`
	APIKeyCiphertext  string `json:"-" gorm:"type:text"`
	APIKeyVersion     string `json:"-" gorm:"type:varchar(50)"`
	APIKeyFingerprint string `json:"api_key_fingerprint,omitempty" gorm:"type:char(64)"`
	TimeoutSeconds    int    `json:"timeout_seconds" gorm:"not null;default:120"`
	MaxOutputTokens   int    `json:"max_output_tokens" gorm:"not null;default:2048"`
	Enabled           bool   `json:"enabled" gorm:"not null;default:false;index"`
	IsPrimary         bool   `json:"is_primary" gorm:"not null;default:false;index"`
	CreatedAt         int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt         int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

func (AssistantModelProfile) TableName() string {
	return "assistant_model_profiles"
}
