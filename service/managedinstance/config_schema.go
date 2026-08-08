package managedinstance

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/model"
)

const managedConfigSchemaVersion = 1

type ConfigFieldSchema struct {
	Key         string `json:"key"`
	RemoteKey   string `json:"remote_key"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Min         *int64 `json:"min,omitempty"`
	Max         *int64 `json:"max,omitempty"`
	MaxLength   int    `json:"max_length,omitempty"`
	MinLength   int    `json:"min_length,omitempty"`
	Format      string `json:"format,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
}

type ConfigSchema struct {
	Kind    string              `json:"kind"`
	Version int                 `json:"version"`
	Fields  []ConfigFieldSchema `json:"fields"`
}

var managedConfigSchemas = map[string]ConfigSchema{
	model.ManagedInstanceKindNewAPI:   newAPIConfigSchema(model.ManagedInstanceKindNewAPI),
	model.ManagedInstanceKindHuichuan: newAPIConfigSchema(model.ManagedInstanceKindHuichuan),
	model.ManagedInstanceKindSub2API: {
		Kind: model.ManagedInstanceKindSub2API, Version: managedConfigSchemaVersion,
		Fields: []ConfigFieldSchema{
			{Key: "ui.site_name", RemoteKey: "site_name", Type: "string", Description: "Public site name", MinLength: 1, MaxLength: 64},
			{Key: "ui.logo_url", RemoteKey: "site_logo", Type: "string", Description: "Public logo URL", MaxLength: 512, Format: "https_url_or_empty"},
			{Key: "ui.site_subtitle", RemoteKey: "site_subtitle", Type: "string", Description: "Public site subtitle", MaxLength: 160},
			{Key: "ui.docs_url", RemoteKey: "doc_url", Type: "string", Description: "Public documentation URL", MaxLength: 512, Format: "https_url_or_empty"},
			{Key: "ui.compact_home", RemoteKey: "compact_home_enabled", Type: "boolean", Description: "Use the compact public home page"},
			{Key: "ui.table_page_size", RemoteKey: "table_default_page_size", Type: "integer", Description: "Default admin table page size", Enum: []any{10, 20, 50, 100}},
		},
	},
}

func newAPIConfigSchema(kind string) ConfigSchema {
	return ConfigSchema{
		Kind: kind, Version: managedConfigSchemaVersion,
		Fields: []ConfigFieldSchema{
			{Key: "ui.site_name", RemoteKey: "SystemName", Type: "string", Description: "Public system name", MinLength: 1, MaxLength: 64},
			{Key: "ui.logo_url", RemoteKey: "Logo", Type: "string", Description: "Public logo URL", MaxLength: 512, Format: "https_url_or_empty"},
		},
	}
}

func ConfigSchemaForKind(kind string) (ConfigSchema, error) {
	schema, ok := managedConfigSchemas[strings.TrimSpace(kind)]
	if !ok {
		return ConfigSchema{}, ErrUnsupportedCapability
	}
	schema.Fields = append([]ConfigFieldSchema(nil), schema.Fields...)
	return schema, nil
}

func validateConfigValues(kind string, schemaVersion int, raw json.RawMessage) (map[string]any, error) {
	schema, err := ConfigSchemaForKind(kind)
	if err != nil {
		return nil, err
	}
	if schemaVersion != schema.Version {
		return nil, fmt.Errorf("%w: unsupported config schema version", ErrInvalidConfigTemplate)
	}
	var input map[string]json.RawMessage
	if err := decodeStrictJSON(raw, &input); err != nil || len(input) == 0 {
		return nil, fmt.Errorf("%w: values must be a non-empty object", ErrInvalidConfigTemplate)
	}
	fields := configFieldMap(schema)
	result := make(map[string]any, len(input))
	for key, value := range input {
		field, ok := fields[key]
		if !ok {
			return nil, fmt.Errorf("%w: field %q is not whitelisted", ErrInvalidConfigTemplate, key)
		}
		normalized, err := normalizeConfigValue(field, value, false)
		if err != nil {
			return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidConfigTemplate, key, err)
		}
		result[key] = normalized
	}
	return result, nil
}

func normalizeRemoteConfig(kind string, desired map[string]any, remote map[string]any) (map[string]any, error) {
	schema, err := ConfigSchemaForKind(kind)
	if err != nil {
		return nil, err
	}
	fields := configFieldMap(schema)
	result := make(map[string]any, len(desired))
	for key := range desired {
		field, ok := fields[key]
		if !ok {
			return nil, ErrInvalidConfigTemplate
		}
		value, exists := remote[field.RemoteKey]
		if !exists {
			return nil, fmt.Errorf("%w: remote field %q is missing", ErrRemoteConfigInvalid, field.RemoteKey)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		normalized, err := normalizeConfigValue(field, encoded, kind == model.ManagedInstanceKindNewAPI || kind == model.ManagedInstanceKindHuichuan)
		if err != nil {
			return nil, fmt.Errorf("%w: remote field %q: %v", ErrRemoteConfigInvalid, field.RemoteKey, err)
		}
		result[key] = normalized
	}
	return result, nil
}

func configFieldMap(schema ConfigSchema) map[string]ConfigFieldSchema {
	result := make(map[string]ConfigFieldSchema, len(schema.Fields))
	for _, field := range schema.Fields {
		result[field.Key] = field
	}
	return result
}

func normalizeConfigValue(field ConfigFieldSchema, raw json.RawMessage, remoteString bool) (any, error) {
	switch field.Type {
	case "string":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("must be a string")
		}
		value = strings.TrimSpace(value)
		if len(value) < field.MinLength {
			return nil, fmt.Errorf("must contain at least %d bytes", field.MinLength)
		}
		if field.MaxLength > 0 && len(value) > field.MaxLength {
			return nil, fmt.Errorf("must contain at most %d bytes", field.MaxLength)
		}
		if field.Format == "https_url_or_empty" && value != "" {
			parsed, err := url.Parse(value)
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
				return nil, fmt.Errorf("must be an absolute HTTPS URL or empty")
			}
		}
		if !configEnumContains(field.Enum, value) {
			return nil, fmt.Errorf("must be one of the allowed values")
		}
		return value, nil
	case "integer":
		var value int64
		if remoteString {
			var text string
			if err := json.Unmarshal(raw, &text); err != nil {
				return nil, fmt.Errorf("must be an integer string")
			}
			parsed, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("must be an integer string")
			}
			value = parsed
		} else if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("must be an integer")
		}
		if field.Min != nil && value < *field.Min || field.Max != nil && value > *field.Max {
			return nil, fmt.Errorf("is outside the allowed range")
		}
		if !configEnumContains(field.Enum, int(value)) {
			return nil, fmt.Errorf("must be one of the allowed values")
		}
		return int(value), nil
	case "boolean":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("must be a boolean")
		}
		return value, nil
	default:
		return nil, fmt.Errorf("has unsupported schema type")
	}
}

func configEnumContains(values []any, candidate any) bool {
	if len(values) == 0 {
		return true
	}
	candidateJSON, _ := json.Marshal(candidate)
	for _, value := range values {
		valueJSON, _ := json.Marshal(value)
		if string(valueJSON) == string(candidateJSON) {
			return true
		}
	}
	return false
}
