package generation

import (
	"wty5.cn/ppt-gen/internal/llm"
	"wty5.cn/ppt-gen/internal/store"
)

func createOutlineToolDefinition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "create_outline",
		Description: "Return the PPT outline as structured data.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":    map[string]string{"type": "string"},
				"theme":    map[string]string{"type": "string"},
				"audience": map[string]string{"type": "string"},
				"style":    map[string]string{"type": "string"},
				"slides": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":         map[string]string{"type": "string"},
							"title":      map[string]string{"type": "string"},
							"purpose":    map[string]string{"type": "string"},
							"keyPoints":  map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
							"visualHint": map[string]string{"type": "string"},
						},
						"required": []string{"id", "title", "purpose", "keyPoints", "visualHint"},
					},
				},
			},
			"required": []string{"title", "theme", "audience", "style", "slides"},
		},
	}
}

type showSVGToolInput struct {
	SlideID   string `json:"slideId"`
	Title     string `json:"title"`
	SVGBase64 string `json:"svgBase64"`
}

func showSVGToolDefinition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "show_svg",
		Description: "Return one generated PPT slide SVG as base64 encoded complete SVG document.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"slideId":   map[string]string{"type": "string"},
				"title":     map[string]string{"type": "string"},
				"svgBase64": map[string]string{"type": "string", "description": "Base64 encoded complete SVG document."},
			},
			"required": []string{"slideId", "title", "svgBase64"},
		},
	}
}

func roleModelConfig(config store.ModelConfig) llm.Config {
	return llm.Config{
		Provider: llm.Provider(config.Provider),
		APIKey:   config.APIKey,
		BaseURL:  config.BaseURL,
		Model:    config.Model,
	}
}
