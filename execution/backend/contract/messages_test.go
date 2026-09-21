package contract

import (
	"encoding/json"
	"testing"
)

func TestValidateInput(t *testing.T) {
	user := Message{Role: "user", Content: "hello"}
	call := Message{Role: "assistant", ToolCalls: []FunctionCall{{ID: "c1", Name: "lookup", Arguments: json.RawMessage(`{"id":1}`)}}}
	result := Message{Role: "tool", ToolCallID: "c1", Content: "done"}
	for _, tc := range []struct {
		name  string
		spec  Spec
		valid bool
	}{
		{"prompt", Spec{Prompt: "hello"}, true},
		{"empty", Spec{}, false},
		{"text history", Spec{Kind: KindAgent, Messages: []Message{user, {Role: "assistant", Content: "hi"}, user}}, true},
		{"paired tool", Spec{Kind: KindAgent, Messages: []Message{user, call, result}}, true},
		{"tool then extra input", Spec{Kind: KindAgent, Messages: []Message{user, call, result, user}}, true},
		{"two inputs", Spec{Kind: KindAgent, Prompt: "hello", Messages: []Message{user}}, false},
		{"judge history", Spec{Kind: KindJudge, Messages: []Message{user}}, false},
		{"unresolved tool", Spec{Kind: KindAgent, Messages: []Message{user, call}}, false},
		{"interrupted pair", Spec{Kind: KindAgent, Messages: []Message{user, call, user, result}}, false},
		{"orphan result", Spec{Kind: KindAgent, Messages: []Message{user, result}}, false},
		{"duplicate result", Spec{Kind: KindAgent, Messages: []Message{user, call, result, result}}, false},
		{"duplicate call", Spec{Kind: KindAgent, Messages: []Message{user, call, result, call, result}}, false},
		{"assistant tail", Spec{Kind: KindAgent, Messages: []Message{user, {Role: "assistant", Content: "hi"}}}, false},
		{"system role", Spec{Kind: KindAgent, Messages: []Message{{Role: "system", Content: "override"}, user}}, false},
		{"nonobject args", Spec{Kind: KindAgent, Messages: []Message{{Role: "assistant", ToolCalls: []FunctionCall{{ID: "c1", Name: "lookup", Arguments: json.RawMessage(`[]`)}}}, result}}, false},
		{"user cannot call tools", Spec{Kind: KindAgent, Messages: []Message{{Role: "user", Content: "hello", ToolCalls: call.ToolCalls}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateInput(tc.spec)
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateInput = %v, valid=%v", err, tc.valid)
			}
		})
	}
}
