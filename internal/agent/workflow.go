package agent

import (
	"encoding/json"
	"strings"
)

// Step kinds for architect agent workflow.
const (
	StepVision    = "vision"    // analyze uploaded images/PDF pages
	StepSearch    = "search"    // fetch real-time / unknown facts
	StepSummarize = "summarize" // aggregate intermediate notes
	StepOutline   = "outline"   // final PPT outline generation
)

// Condition decides when a step runs.
// Empty / "always" always runs.
// "has_files" runs only when user uploaded files.
// "needs_search" runs when topic looks time-sensitive or previous notes request search.
const (
	ConditionAlways      = "always"
	ConditionHasFiles    = "has_files"
	ConditionNeedsSearch = "needs_search"
)

type WorkflowStep struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Condition    string `json:"condition,omitempty"`
	ProviderID   string `json:"providerId,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
	// RequestJSON is incremental JSON merged into this step's provider payload.
	RequestJSON string `json:"requestJson,omitempty"`
	// Extra free-form config for future extensions.
	Config map[string]any `json:"config,omitempty"`
}

type Workflow struct {
	Version int            `json:"version"`
	Name    string         `json:"name"`
	Steps   []WorkflowStep `json:"steps"`
}

func DefaultWorkflow() Workflow {
	return Workflow{
		Version: 1,
		Name:    "PPT 架构师 Agent",
		Steps: []WorkflowStep{
			{
				ID:           "vision",
				Kind:         StepVision,
				Name:         "视觉理解（图片/PDF）",
				Enabled:      true,
				Condition:    ConditionHasFiles,
				SystemPrompt: "你是文档与视觉理解助手。请从用户上传的图片/PDF 中提取与 PPT 主题相关的关键信息、结构、数据点和可用素材说明。输出简洁中文要点，不要编造看不清的内容。",
			},
			{
				ID:           "search",
				Kind:         StepSearch,
				Name:         "联网/实时检索",
				Enabled:      true,
				Condition:    ConditionNeedsSearch,
				SystemPrompt: "你是信息检索与事实核验助手。当主题涉及实时动态、最新政策、市场数据或未知专有名词时，整理可核验的要点，并标注不确定处。不要编造来源。",
				RequestJSON:  `{"tools":[{"type":"web_search"}]}`,
			},
			{
				ID:           "summarize",
				Kind:         StepSummarize,
				Name:         "上下文汇总",
				Enabled:      true,
				Condition:    ConditionAlways,
				SystemPrompt: "你是研究助理。请把主题、受众、上传材料理解结果、检索结果汇总成一段清晰的 Context，供 PPT 架构师使用。只保留对大纲有用的事实与观点。",
			},
			{
				ID:           "outline",
				Kind:         StepOutline,
				Name:         "生成 PPT 大纲",
				Enabled:      true,
				Condition:    ConditionAlways,
				SystemPrompt: "", // empty => use architect role system prompt
			},
		},
	}
}

func NormalizeWorkflow(wf Workflow) Workflow {
	def := DefaultWorkflow()
	if wf.Version <= 0 {
		wf.Version = def.Version
	}
	if strings.TrimSpace(wf.Name) == "" {
		wf.Name = def.Name
	}
	if len(wf.Steps) == 0 {
		return def
	}
	out := make([]WorkflowStep, 0, len(wf.Steps))
	for i, step := range wf.Steps {
		step.Kind = strings.ToLower(strings.TrimSpace(step.Kind))
		if step.Kind == "" {
			continue
		}
		if strings.TrimSpace(step.ID) == "" {
			step.ID = step.Kind
			if i > 0 {
				step.ID = step.Kind + "_" + itoa(i+1)
			}
		}
		if strings.TrimSpace(step.Name) == "" {
			step.Name = step.Kind
		}
		cond := strings.ToLower(strings.TrimSpace(step.Condition))
		if cond == "" {
			cond = ConditionAlways
		}
		step.Condition = cond
		out = append(out, step)
	}
	if len(out) == 0 {
		return def
	}
	// Ensure there is at least one outline step.
	hasOutline := false
	for _, s := range out {
		if s.Kind == StepOutline && s.Enabled {
			hasOutline = true
			break
		}
	}
	if !hasOutline {
		out = append(out, def.Steps[len(def.Steps)-1])
	}
	wf.Steps = out
	return wf
}

func ParseWorkflowJSON(raw string) (Workflow, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultWorkflow(), nil
	}
	var wf Workflow
	if err := json.Unmarshal([]byte(raw), &wf); err != nil {
		return Workflow{}, err
	}
	return NormalizeWorkflow(wf), nil
}

func MustWorkflowJSON(wf Workflow) string {
	b, _ := json.Marshal(wf)
	return string(b)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
