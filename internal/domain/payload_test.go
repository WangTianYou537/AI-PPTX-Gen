package domain

import "testing"

func TestRenderPayloadTemplatePlaceholders(t *testing.T) {
	raw := `{
	  "model": "{{model}}",
	  "instructions": "{{systemPrompt}}",
	  "input": [{"role":"user","content":"{{userPrompt}}"}],
	  "messages": "{{messages}}",
	  "json": "{{jsonMode}}"
	}`
	out, err := RenderPayloadTemplate(raw, PayloadVars{
		Model:        "gpt-test",
		SystemPrompt: "sys",
		UserPrompt:   "hello",
		Messages:     []any{map[string]any{"role": "user", "content": "hello"}},
		JSONMode:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["model"] != "gpt-test" {
		t.Fatalf("model=%v", out["model"])
	}
	if out["instructions"] != "sys" {
		t.Fatalf("instructions=%v", out["instructions"])
	}
	input, ok := out["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input=%v", out["input"])
	}
	msgs, ok := out["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages not structured: %T %v", out["messages"], out["messages"])
	}
	if out["json"] != true {
		t.Fatalf("jsonMode=%v", out["json"])
	}
}

func TestRenderPayloadTemplateRequiresObject(t *testing.T) {
	if _, err := RenderPayloadTemplate(`["x"]`, PayloadVars{}); err == nil {
		t.Fatal("expected error for non-object payload")
	}
}
