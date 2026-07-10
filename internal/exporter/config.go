package exporter

import (
	"encoding/json"
	"fmt"
	"strings"

	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
)

// ValidatePluginConfig checks an exporter's config JSON against a plugin's
// declared config schema (a JSON array of pluginsdk.ConfigField). It verifies
// that required fields are present and that declared fields have the right JSON
// type. Unknown extra keys are allowed. A nil/empty schema means "no schema
// declared" and always passes.
func ValidatePluginConfig(schemaJSON []byte, configJSON []byte) error {
	if len(schemaJSON) == 0 || string(schemaJSON) == "null" {
		return nil
	}

	var schema []pluginsdk.ConfigField
	if err := json.Unmarshal(schemaJSON, &schema); err != nil || len(schema) == 0 {
		return nil // malformed or empty schema: don't block
	}

	config := map[string]any{}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &config); err != nil {
			return fmt.Errorf("plugin config is not valid JSON: %w", err)
		}
	}

	var problems []string
	for _, field := range schema {
		value, present := config[field.Name]
		if !present || value == nil {
			if field.Required {
				problems = append(problems, fmt.Sprintf("%q is required", field.Name))
			}
			continue
		}

		switch field.Type {
		case pluginsdk.NumberField:
			if _, ok := value.(float64); !ok { // JSON numbers decode to float64
				problems = append(problems, fmt.Sprintf("%q must be a number", field.Name))
			}
		case pluginsdk.BoolField:
			if _, ok := value.(bool); !ok {
				problems = append(problems, fmt.Sprintf("%q must be a boolean", field.Name))
			}
		case pluginsdk.StringField:
			if _, ok := value.(string); !ok {
				problems = append(problems, fmt.Sprintf("%q must be a string", field.Name))
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid plugin config: %s", strings.Join(problems, "; "))
	}
	return nil
}
