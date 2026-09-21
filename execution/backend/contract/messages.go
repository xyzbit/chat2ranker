package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Message preserves one user input, assistant output, or caller tool result.
// ToolCalls belong to assistant messages; ToolCallID belongs to tool messages.
type Message struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []FunctionCall `json:"toolCalls,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
}

// FunctionCall is an observed assistant request, never authorization to execute
// that historical call again. Arguments retains the original JSON object.
type FunctionCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ValidateInput requires exactly one input representation and a complete,
// ordered tool-call/result history ending in new user or tool input.
func ValidateInput(spec Spec) error {
	if len(spec.Messages) == 0 {
		if strings.TrimSpace(spec.Prompt) == "" {
			return errors.New("prompt or messages is required")
		}
		return nil
	}
	if spec.Prompt != "" || spec.Kind != KindAgent {
		return errors.New("messages is only valid for agent executions without prompt")
	}
	pending, used := map[string]bool{}, map[string]bool{}
	for index, message := range spec.Messages {
		if message.Role != "tool" && len(pending) > 0 {
			return fmt.Errorf("message %d precedes the pending tool results", index)
		}
		switch message.Role {
		case "user":
			if strings.TrimSpace(message.Content) == "" || len(message.ToolCalls) > 0 || message.ToolCallID != "" {
				return fmt.Errorf("message %d is not a user text message", index)
			}
		case "assistant":
			if message.ToolCallID != "" || (strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0) {
				return fmt.Errorf("message %d is not an assistant message", index)
			}
			for _, call := range message.ToolCalls {
				var arguments map[string]json.RawMessage
				if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" || used[call.ID] || json.Unmarshal(call.Arguments, &arguments) != nil || arguments == nil {
					return fmt.Errorf("message %d contains an invalid or repeated tool call", index)
				}
				pending[call.ID], used[call.ID] = true, true
			}
		case "tool":
			if !pending[message.ToolCallID] || len(message.ToolCalls) > 0 || strings.TrimSpace(message.Content) == "" {
				return fmt.Errorf("message %d has no matching pending tool call or valid output", index)
			}
			delete(pending, message.ToolCallID)
		default:
			return fmt.Errorf("message %d uses unsupported role %q", index, message.Role)
		}
	}
	if len(pending) > 0 || spec.Messages[len(spec.Messages)-1].Role == "assistant" {
		return errors.New("messages must end in user or tool input with no pending calls")
	}
	return nil
}
