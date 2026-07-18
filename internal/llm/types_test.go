package llm

import (
	"reflect"
	"testing"
)

func TestMergePayloadJSON(t *testing.T) {
	base := map[string]any{
		"model":  "gpt-test",
		"stream": true,
		"nested": map[string]any{"a": 1},
	}

	merged, err := mergePayloadJSON(base, map[string]any{
		"temperature": 0.2,
		"stream":      false,
	})
	if err != nil {
		t.Fatalf("mergePayloadJSON returned error: %v", err)
	}

	got, ok := merged.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", merged)
	}
	if got["model"] != "gpt-test" {
		t.Fatalf("model overwritten unexpectedly: %#v", got["model"])
	}
	if got["stream"] != false {
		t.Fatalf("expected extra stream=false to win, got %#v", got["stream"])
	}
	if got["temperature"] != 0.2 {
		t.Fatalf("expected temperature from extra, got %#v", got["temperature"])
	}

	empty, err := mergePayloadJSON(base, nil)
	if err != nil {
		t.Fatalf("nil extra should not error: %v", err)
	}
	if !reflect.DeepEqual(empty, base) {
		t.Fatalf("nil extra should return base unchanged")
	}
}
