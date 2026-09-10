package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuredOutputValidation(t *testing.T) {
	spec := &StructuredOutput{Name: "answer", Schema: json.RawMessage(`{
		"type":"object",
		"properties":{"answer":{"type":"string"},"count":{"type":"integer"}},
		"required":["answer"],
		"additionalProperties":false
	}`)}
	if err := ValidateStructuredOutput(spec); err != nil {
		t.Fatal(err)
	}
	value, err := DecodeStructuredOutput(spec, `{"answer":"done","count":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != `{"answer":"done","count":2}` {
		t.Fatalf("normalized output = %s", value)
	}
	for _, output := range []string{`{"answer":4}`, `{"answer":"done","extra":true}`, "```json\n{}\n```"} {
		if _, err := DecodeStructuredOutput(spec, output); err == nil {
			t.Fatalf("invalid output accepted: %s", output)
		}
	}
}

func TestStructuredOutputSchemaRejectsExternalReferencesAndOversize(t *testing.T) {
	for _, schema := range []string{
		`{"$ref":"https://example.com/schema.json"}`,
		`{"$dynamicRef":"other.json#value"}`,
		`true`,
		`{"type":"missing"}`,
	} {
		if err := ValidateStructuredOutput(&StructuredOutput{Name: "answer", Schema: json.RawMessage(schema)}); err == nil {
			t.Fatalf("invalid schema accepted: %s", schema)
		}
	}
	oversized := `{"description":"` + strings.Repeat("x", maxStructuredOutputSchemaBytes) + `"}`
	if err := ValidateStructuredOutput(&StructuredOutput{Name: "answer", Schema: json.RawMessage(oversized)}); err == nil {
		t.Fatal("oversized schema accepted")
	}
}
