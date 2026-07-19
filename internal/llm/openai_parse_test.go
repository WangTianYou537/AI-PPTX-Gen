package llm

import "testing"

func TestParseOpenAIChatCompletionFlexibleVariants(t *testing.T) {
	cases := []string{
		`{"choices":[{"message":{"content":"<svg>ok</svg>"}}]}`,
		`{"choices":[{"message":{"content":[{"type":"text","text":"<svg>ok</svg>"}]}}]}`,
		`{"choices":[{"text":"<svg>ok</svg>"}]}`,
		`{"content":"<svg>ok</svg>"}`,
		`{"output":"<svg>ok</svg>"}`,
	}
	for i, body := range cases {
		resp, err := parseOpenAIChatCompletionFlexible([]byte(body))
		if err != nil {
			t.Fatalf("case %d err=%v", i, err)
		}
		if resp.Text != "<svg>ok</svg>" {
			t.Fatalf("case %d text=%q", i, resp.Text)
		}
	}
}

func TestForceStreamFlag(t *testing.T) {
	body := map[string]any{"model": "m", "stream": true}
	out := forceStreamFlag(body, false).(map[string]any)
	if out["stream"] != false {
		t.Fatalf("stream=%v", out["stream"])
	}
	if out["model"] != "m" {
		t.Fatalf("model lost")
	}
}
