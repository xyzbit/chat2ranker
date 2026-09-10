package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

const maxCallerFunctionTools = 16

var functionNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

func normalizeCallerTools(raw json.RawMessage) ([]domain.ResponseFunctionTool, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 || len(entries) > maxCallerFunctionTools {
		return nil, problem(400, "invalid_tools", fmt.Sprintf("tools must contain 1 to %d function definitions", maxCallerFunctionTools))
	}
	result := make([]domain.ResponseFunctionTool, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		var tool struct {
			Type            string          `json:"type"`
			Name            string          `json:"name"`
			Description     string          `json:"description"`
			Parameters      json.RawMessage `json:"parameters"`
			Strict          *bool           `json:"strict"`
			RequireApproval bool            `json:"require_approval"`
		}
		if err := decodeServingJSON(entry, &tool); err != nil {
			return nil, problem(400, "invalid_tools", "tool definitions contain invalid or unsupported fields")
		}
		tool.Type, tool.Name, tool.Description = strings.TrimSpace(tool.Type), strings.TrimSpace(tool.Name), strings.TrimSpace(tool.Description)
		if tool.Type != "function" || !functionNamePattern.MatchString(tool.Name) || seen[tool.Name] || len(tool.Description) > 512 {
			return nil, problem(400, "invalid_tools", "each tool requires a unique function name and an optional description of at most 512 characters")
		}
		if tool.Strict != nil && !*tool.Strict {
			return nil, problem(400, "invalid_tools", "caller function tools require strict=true")
		}
		var schema any
		if len(tool.Parameters) == 0 || json.Unmarshal(tool.Parameters, &schema) != nil {
			return nil, problem(400, "invalid_tools", "caller function tools require a valid parameters object schema")
		}
		parameters, err := json.Marshal(schema)
		if err != nil {
			return nil, err
		}
		if err := contract.ValidateStructuredOutput(&contract.StructuredOutput{Name: tool.Name, Schema: parameters}); err != nil {
			return nil, problem(400, "invalid_tools", err.Error())
		}
		seen[tool.Name] = true
		result = append(result, domain.ResponseFunctionTool{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: parameters, Strict: true, RequireApproval: tool.RequireApproval})
	}
	return result, nil
}

func sameCallerTools(left, right []domain.ResponseFunctionTool) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func callerToolPrompt(prompt string, tools []domain.ResponseFunctionTool) string {
	if len(tools) == 0 {
		return prompt
	}
	definitions, _ := json.Marshal(tools)
	return strings.Join([]string{
		"Available caller functions (JSON):\n" + string(definitions),
		"When a caller function is required, return only one JSON object with type=\"function_call\", the exact function name, and an arguments object matching its parameters. Otherwise answer normally. Never claim that you executed a caller function.",
		"Task:\n" + prompt,
	}, "\n\n")
}

func parseCallerToolCall(output string, tools []domain.ResponseFunctionTool) (*domain.ResponseToolCall, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	var envelope struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	raw := json.RawMessage(strings.TrimSpace(output))
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.Type != "function_call" {
		return nil, nil
	}
	if decodeServingJSON(raw, &envelope) != nil || envelope.Name == "" || len(envelope.Arguments) == 0 {
		return nil, problem(500, "invalid_function_call", "Agent returned a malformed caller function request")
	}
	for _, tool := range tools {
		if tool.Name != envelope.Name {
			continue
		}
		validated, err := contract.DecodeStructuredOutput(&contract.StructuredOutput{Name: tool.Name, Schema: tool.Parameters}, string(envelope.Arguments))
		if err != nil {
			return nil, problem(500, "invalid_function_call", err.Error())
		}
		return &domain.ResponseToolCall{ID: newServingID("call"), Name: tool.Name, Arguments: validated, ApprovalRequired: tool.RequireApproval}, nil
	}
	return nil, problem(500, "invalid_function_call", "Agent requested an undeclared caller function")
}

func functionCallOutputs(items []domain.ResponseInputItem) []domain.ResponseInputItem {
	result := []domain.ResponseInputItem{}
	for _, item := range items {
		if item.Type == "function_call_output" {
			result = append(result, item)
		}
	}
	return result
}

func validateFunctionContinuation(previous domain.ServingResponse, input normalizedServingInput) error {
	outputs := functionCallOutputs(input.Items)
	if previous.ToolCall == nil {
		if len(outputs) > 0 {
			return problem(400, "unexpected_function_call_output", "previous Response did not request a caller function")
		}
		return nil
	}
	if previous.ToolCall.ApprovalRequired && previous.ToolCall.ApprovalStatus != "approved" {
		return problem(409, "function_call_not_approved", "previous function call must be approved before submitting its output")
	}
	if len(outputs) != 1 || outputs[0].CallID != previous.ToolCall.ID {
		return problem(400, "function_call_output_required", "continuation requires one function_call_output for the previous Response call_id")
	}
	return nil
}
