package harness

import (
	"testing"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

func TestDSHInvocationPatchesIncludeProviderWithModel(t *testing.T) {
	patches := dshInvocationPatches(contract.Spec{
		Model:        "deepseek-chat",
		SystemPrompt: "Answer briefly.",
	})
	if len(patches) != 3 {
		t.Fatalf("expected model, system-prompt, and title patches, got %#v", patches)
	}
	modelConfig, ok := patches[0]["config"].(map[string]any)
	if !ok {
		t.Fatalf("model patch has unexpected config: %#v", patches[0])
	}
	if modelConfig["provider"] != "deepseek-official" || modelConfig["model"] != "deepseek-chat" {
		t.Fatalf("model patch must include DSH provider and model: %#v", modelConfig)
	}
	if patches[2]["id"] != "session-title-llm" || patches[2]["disabled"] != true {
		t.Fatalf("DSH ranking invocation must disable LLM session titles: %#v", patches[2])
	}
}

func TestDSHModelConnectionProtocols(t *testing.T) {
	for protocol, want := range map[string]string{
		contract.ProtocolOpenAIChat:      "openai-completions",
		contract.ProtocolOpenAIResponses: "openai-responses",
		contract.ProtocolAnthropic:       "anthropic-messages",
	} {
		if got := dshAPI(protocol); got != want {
			t.Fatalf("%s: got %q, want %q", protocol, got, want)
		}
	}
}
