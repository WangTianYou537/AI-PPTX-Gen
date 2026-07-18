package domain

import (
	"encoding/json"
	"errors"
	"strings"

	"wty5.cn/ppt-gen/internal/ppt"
)

// NormalizePromptSettings fills legacy/default system prompts when empty.
// ArchitectSystemPrompt/SVGSystemPrompt are legacy fallbacks from older stores.
func NormalizePromptSettings(architectSystem, svgSystem, legacyArchitect, legacySVG string) (string, string) {
	if architectSystem == "" {
		architectSystem = legacyArchitect
	}
	if svgSystem == "" {
		svgSystem = legacySVG
	}
	if architectSystem == "" {
		architectSystem = ppt.DefaultArchitectSystemPrompt
	}
	if svgSystem == "" {
		svgSystem = ppt.DefaultSVGSystemPrompt
	}
	return architectSystem, svgSystem
}

// ParseRequestJSON parses optional admin-provided request body extensions.
func ParseRequestJSON(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, errors.New("请求 JSON 格式不正确: " + err.Error())
	}
	if payload == nil {
		return map[string]any{}, nil
	}
	return payload, nil
}

// ValidateRequestJSON ensures admin request JSON is empty or a JSON object.
func ValidateRequestJSON(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return errors.New("请求 JSON 格式不正确")
	}
	if payload == nil {
		return errors.New("请求 JSON 必须是对象")
	}
	return nil
}
