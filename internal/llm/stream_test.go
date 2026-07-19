package llm

import "testing"

func TestPayloadWantsStream(t *testing.T) {
	if !payloadWantsStream(map[string]any{"stream": true}) {
		t.Fatal("expected true")
	}
	if payloadWantsStream(map[string]any{"stream": false}) {
		t.Fatal("expected false")
	}
	if payloadWantsStream(map[string]any{}) {
		t.Fatal("missing stream should be false")
	}
	if !payloadWantsStream(map[string]any{"stream": "true"}) {
		t.Fatal("string true should count")
	}
}

func TestParseOpenAIChatCompletionToolAndText(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"hello","tool_calls":[{"function":{"name":"create_outline","arguments":"{\"a\":1}"}}]}}]}`)
	resp, err := parseOpenAIChatCompletion(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.ToolName != "create_outline" {
		t.Fatalf("tool=%s", resp.ToolName)
	}
	if string(resp.ToolInput) != `{"a":1}` {
		t.Fatalf("args=%s", string(resp.ToolInput))
	}
}
