package config

import (
	_ "embed"
	"fmt"
	"sync"

	yamlvalidator "github.com/Yakwilik/go-yamlvalidator"
)

//go:embed release-notes.schema.json
var schemaJSON []byte

var configurationValidator = sync.OnceValues(func() (*yamlvalidator.Validator, error) {
	schema, err := yamlvalidator.CompileJSONSchema(schemaJSON)
	if err != nil {
		return nil, fmt.Errorf("compile embedded configuration schema: %w", err)
	}
	schema.Required = true
	return yamlvalidator.NewValidator(schema), nil
})

func validateStructure(data []byte) error {
	validator, err := configurationValidator()
	if err != nil {
		return err
	}
	result := validator.ValidateWithOptions(data, yamlvalidator.ValidationContext{MaxDocuments: 1})
	if result.HasErrors() {
		return fmt.Errorf("configuration schema validation failed:\n%s", result.FormatAll(true))
	}
	return nil
}
