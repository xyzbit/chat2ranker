package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const maxStructuredOutputSchemaBytes = 64 << 10

// ValidateStructuredOutput compiles a trusted Execution output requirement.
// Remote references are rejected so validation never performs network or
// filesystem reads.
func ValidateStructuredOutput(spec *StructuredOutput) error {
	if spec == nil {
		return nil
	}
	if strings.TrimSpace(spec.Name) == "" {
		return errors.New("structured output name is required")
	}
	_, err := compileStructuredOutput(spec.Schema)
	return err
}

// DecodeStructuredOutput parses and validates the complete Harness output.
// It returns a compact JSON representation for durable result storage.
func DecodeStructuredOutput(spec *StructuredOutput, output string) (json.RawMessage, error) {
	if spec == nil {
		return nil, errors.New("structured output requirement is missing")
	}
	schema, err := compileStructuredOutput(spec.Schema)
	if err != nil {
		return nil, err
	}
	value, err := jsonschema.UnmarshalJSON(strings.NewReader(output))
	if err != nil {
		return nil, fmt.Errorf("structured output is not one JSON value: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return nil, fmt.Errorf("structured output does not match schema: %w", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode structured output: %w", err)
	}
	return encoded, nil
}

func compileStructuredOutput(raw json.RawMessage) (*jsonschema.Schema, error) {
	if len(raw) == 0 {
		return nil, errors.New("structured output schema is required")
	}
	if len(raw) > maxStructuredOutputSchemaBytes {
		return nil, fmt.Errorf("structured output schema exceeds %d bytes", maxStructuredOutputSchemaBytes)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("structured output schema is invalid JSON: %w", err)
	}
	if _, ok := document.(map[string]any); !ok {
		return nil, errors.New("structured output schema must be a JSON object")
	}
	if err := rejectExternalSchemaReferences(document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("structured-output.json", document); err != nil {
		return nil, fmt.Errorf("register structured output schema: %w", err)
	}
	compiled, err := compiler.Compile("structured-output.json")
	if err != nil {
		return nil, fmt.Errorf("compile structured output schema: %w", err)
	}
	return compiled, nil
}

func rejectExternalSchemaReferences(value any) error {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if key == "$ref" || key == "$dynamicRef" || key == "$recursiveRef" {
				reference, ok := child.(string)
				if !ok || !strings.HasPrefix(reference, "#") {
					return fmt.Errorf("structured output %s must be a local fragment reference", key)
				}
			}
			if err := rejectExternalSchemaReferences(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range current {
			if err := rejectExternalSchemaReferences(child); err != nil {
				return err
			}
		}
	}
	return nil
}
