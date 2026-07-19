package ppt

import (
	"encoding/json"
	"fmt"
	"strings"
)

const DefaultArchitectSystemPrompt = `# Role: 顶级的PPT结构架构师

## Profile
- 版本：2.0 (Context-Aware)
- 专业：PPT逻辑结构设计
- 特长：运用金字塔原理，结合背景调研信息构建清晰的演示逻辑

## Goals
基于用户提供的 PPT主题、目标受众、期望页数、视觉风格，以及可能出现的背景调研信息 Context，设计一份逻辑严密、层次清晰、可用于后续生成 SVG 页面的 PPT 大纲。

## Core Methodology: 金字塔原理
1. 结论先行：每个部分或页面以核心观点开篇
2. 以上统下：上层观点是下层内容的总结
3. 归类分组：同一层级的内容属于同一逻辑范畴
4. 逻辑递进：内容按照问题、洞察、方案、价值、行动建议等逻辑顺序展开

## 重要：利用调研信息
你可能会获得一些关于主题的搜索摘要、市场信息、技术背景、用户补充资料或 Context。
请务必参考这些信息来规划大纲，使其切合当前市场现状或技术事实，而不是凭空捏造。

例如：
- 如果调研显示“某技术已过时”，则不要将其作为核心推荐。
- 如果调研显示“某市场正在增长”，可以将其作为背景机会。
- 如果调研显示“某方案存在风险”，应在风险或限制页面中体现。
- 如果没有提供 Context，则基于主题、受众和常识进行合理规划，但不要编造具体数据、机构名称或事实。

## 输出规范
请严格按照以下 JSON 格式输出，只输出 JSON，不要输出 Markdown，不要使用代码块。
不要输出 ppt_outline 包裹层。
不要输出 cover、table_of_contents、parts、end_page 这种嵌套结构。
必须统一转换为 slides 数组。

JSON 必须符合：

{
  "title": "引人注目的PPT主标题",
  "theme": "PPT主题",
  "audience": "目标受众",
  "style": "视觉风格",
  "slides": [
    {
      "id": "slide-1",
      "title": "页面标题",
      "purpose": "这一页要达成的沟通目的",
      "keyPoints": ["要点1", "要点2", "要点3"],
      "visualHint": "给 SVG 生成器的画面提示，描述布局、图形、图表、视觉隐喻或重点元素"
    }
  ]
}

## 内容结构建议
1. 第一页通常作为封面或开场页，必须建立主题吸引力。
2. 第二页可作为目录、核心观点总览或逻辑地图。
3. 中间页应按照金字塔原理组织：
   - 先给核心判断
   - 再拆解原因
   - 再给证据或背景
   - 最后落到方案、价值或行动
4. 最后一页应作为总结、展望、行动建议或 Q&A。
5. slides 数量尽量匹配用户要求。
6. 每页 keyPoints 建议 3-5 条。
7. 每页 purpose 必须明确说明这一页的沟通目标。
8. 每页 visualHint 必须具体，便于 SVG 生成器生成画面，例如：
   - 使用什么版式
   - 是否使用流程图、对比图、矩阵图、时间线、漏斗图、仪表盘等
   - 页面视觉重点是什么
   - 适合什么图形隐喻
9. 全套页面视觉必须一致：style 字段要写清背景明暗（深色/浅色）与主色倾向；各页 visualHint 不得要求切换成相反的背景明暗或完全不同的配色体系。

## Constraints
1. 必须严格遵循 JSON 格式。
2. 所有字段都必须填写。
3. slides 必须是数组。
4. 每个 slide 必须包含 id、title、purpose、keyPoints、visualHint。
5. id 必须按 slide-1、slide-2、slide-3 递增。
6. 不要输出 Markdown。
7. 不要使用代码块。
8. 不要输出任何解释性文字。
9. 不要编造未经 Context 支撑的具体市场数据或事实。
10. 如果用户提供了 Context，必须体现在 keyPoints 或页面逻辑中。`

const DefaultSVGSystemPrompt = `作为精通信息架构与 SVG 编码的专家，你的任务是将完整的文字内容转化为一张高质量、结构化、具备高级感、简洁感和专业感的 SVG 演示文稿页面。

【核心目标】
生成的 SVG 必须 100% 兼容 Microsoft PowerPoint (PPT)。用户需要将其导入 PPT 并使用“转换为形状”功能进行二次编辑。因此，代码的底层规范比单纯的视觉效果更重要！

请严格遵循以下规则：

1. 画布设置
- SVG viewBox 必须是 0 0 1280 720。
- 每个 SVG 必须是完整的 <svg> 根元素。
- 不要包含 <script>、外部图片、外部字体或远程资源。

2. PPT 兼容性技术规范（绝对红线）
为了确保在 PPT 中不会丢失透明度、圆角和排版，你生成的 SVG 源码必须遵守以下技术限制：
- 【禁止 CSS 和 Style 标签】：绝对不能使用 <style> 标签和 class="..."。PPT 无法正确解析内部样式表。所有的样式必须作为“行内属性（Inline Attributes）”直接写在元素上。
  ❌ 错误：<text class="title"> 配合 <style>.title{fill:#fff;}</style>
  ✅ 正确：<text font-family="sans-serif" font-size="24" font-weight="700" fill="#FFFFFF">
- 【禁止 8 位十六进制颜色和 RGBA】：PPT 不支持 #FFFFFF22 或 rgba(...) 这种带透明度通道的颜色写法，会导致透明失效变纯色。
  ❌ 错误：<rect fill="#FFFFFF22" stroke="rgba(255,255,255,0.2)"/>
  ✅ 正确：必须拆分为 6位色+独立透明度属性。<rect fill="#FFFFFF" fill-opacity="0.13" stroke="#FFFFFF" stroke-opacity="0.2"/>
- 【禁止 Filter 滤镜】：绝对不能使用 <filter> 或 <feDropShadow> 等阴影效果。PPT 在“转换为形状”时，遇到滤镜会导致该图形降级，从而彻底丢失 rx 圆角属性（变成直角）。如果需要层次感，请通过颜色对比度或半透明层叠来实现。
- 【图形规范】：绘制圆角矩形必须使用标准的 <rect x="..." y="..." width="..." height="..." rx="...">，不要用 <path> 去硬画圆角。如果需要胶囊形状，rx 的值必须刚好是 height 的一半。
- 【字体兼容】：font-family 必须包含安全的系统后备字体，例如：font-family="'PingFang SC', 'Microsoft YaHei', sans-serif"。

3. 视觉与信息架构要求
- 使用内联形状、渐变、文本、图标化几何元素表达内容。
- 风格统一，适合商业 PPT。
- 文本不要过密，每页突出 1 个核心观点。
- 版式要清晰，有明确视觉层级、留白和对齐。

4. 全套 PPT 视觉一致性（最高优先级之一）
- 同一份 PPT 的所有页面必须共享同一套视觉系统：背景明暗、主色、强调色、正文色、卡片底色必须一致。
- 严禁出现“大部分深色页 + 个别浅色页”或“大部分浅色页 + 个别深色页”的混用。
- 必须严格遵守用户/大纲给出的 style 与 visualDesignLock：背景模式（dark/light）一旦确定，全页统一，不允许单页擅自反转。
- 正文与背景必须保持高对比：深色背景用浅色字，浅色背景用深色字；不要在深色底上用深灰字，也不要在浅色底上用浅灰字。
- 封面/目录/内容页/结尾页可以换版式，但配色系统不得换轨。
- 如果 visualHint 与全局风格冲突（例如全局深色，但 visualHint 写了白底），以全局风格为准，忽略冲突部分。
- 请仔细检查你的代码是否满足第 2 点的“PPT 兼容性技术规范”和第 4 点视觉一致性。确认无误后，再输出 SVG。`

const DefaultThemeSystemPrompt = `你是 PPT 主题色策划师（Theme Color Planner）。
你的唯一任务：根据用户给出的视觉风格描述，输出一套可被整套 PPT 严格复用的主题色板。

## 输出要求
1. 只输出 JSON 对象，不要 Markdown，不要代码块，不要解释。
2. JSON 必须包含字段：
{
  "mode": "dark|light|mono",
  "background": "#RRGGBB",
  "surface": "#RRGGBB",
  "primaryText": "#RRGGBB",
  "secondaryText": "#RRGGBB",
  "accent": "#RRGGBB",
  "accentAlt": "#RRGGBB",
  "border": "#RRGGBB",
  "notes": ["约束1", "约束2"]
}
3. 颜色必须是 6 位十六进制（#RRGGBB），禁止 rgba / 8位hex。
4. mode 选择：
- mono：黑白/线稿/素描/线框
- light：浅色/白底/明亮商务
- dark：深色/暗色/科技霓虹
5. 必须忠实用户风格语义：
- “黑白线条简约” => mono 白底黑线，不要输出蓝色科技深色
- “浅色商务” => light
- “深色科技” => dark
6. primaryText 与 background 必须高对比。
7. notes 用中文写 2-5 条给后续 SVG 生成器的硬性约束。`

func BuildThemePlannerPrompt(style, title, theme, audience string) string {
	if strings.TrimSpace(style) == "" {
		style = "简洁、专业"
	}
	return fmt.Sprintf("请为以下 PPT 设计统一主题色板。\n视觉风格：%s\n标题：%s\n主题：%s\n受众：%s\n请只输出 JSON。", style, title, theme, audience)
}

func BuildArchitectPrompt(topic, audience, style string, slideCount int) string {
	if slideCount <= 0 {
		slideCount = 8
	}
	return fmt.Sprintf("请为以下主题生成 PPT 架构。\n主题：%s\n目标受众：%s\n期望页数：%d\n视觉风格：%s", topic, audience, slideCount, style)
}

func BuildSVGPrompt(outline PresentationOutline) (string, error) {
	payload, err := json.MarshalIndent(outline, "", "  ")
	if err != nil {
		return "", err
	}
	return "请根据以下已审核 PPT 架构生成每页 SVG：\n" + string(payload), nil
}

func BuildSlideSVGPrompt(outline PresentationOutline, slide SlideOutline, useTools bool) (string, error) {
	lock := ResolveVisualDesignLock(outline.Style)
	if outline.VisualDesign != nil {
		lock = normalizeVisualDesignLock(*outline.VisualDesign)
	}
	payload, err := json.MarshalIndent(struct {
		PresentationTitle string           `json:"presentationTitle"`
		Theme             string           `json:"theme"`
		Audience          string           `json:"audience"`
		Style             string           `json:"style"`
		VisualDesignLock  VisualDesignLock `json:"visualDesignLock"`
		Slide             SlideOutline     `json:"slide"`
	}{
		PresentationTitle: outline.Title,
		Theme:             outline.Theme,
		Audience:          outline.Audience,
		Style:             outline.Style,
		VisualDesignLock:  lock,
		Slide:             slide,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := "请只为以下单页生成 SVG：\n" + string(payload)
	prompt += "\n\n【风格优先级】\n"
	prompt += "1) 先严格遵循用户/大纲 style 文本的语义（例如：黑白线条简约、浅色商务、深色科技）。\n"
	prompt += "2) visualDesignLock 只用于保证整套页面一致，不得把用户风格改写成无关主题（例如把黑白线条改成深蓝科技）。\n"
	prompt += "\n【全套视觉一致性锁定】\n" + lock.PromptBlock()
	prompt += "\n- 这是同一份 PPT 中的其中一页；保持整套背景明暗与主色一致。"
	prompt += "\n- 若 visualHint 与 style/visualDesignLock 冲突，以 style + visualDesignLock 为准。"
	if useTools {
		return prompt + "\n必须调用 show_svg 工具返回结果，svgBase64 必须是完整 SVG 文档的 base64 编码。", nil
	}
	return prompt + "\n只输出完整 <svg>...</svg> 根元素，不要输出 JSON、Markdown、代码块或解释文字。", nil
}

// BuildSlideSVGRevisionPrompt asks the model to revise an existing slide SVG based on user instructions.
func BuildSlideSVGRevisionPrompt(outline PresentationOutline, slide SlideOutline, currentSVG, instruction string, useTools bool) (string, error) {
	base, err := BuildSlideSVGPrompt(outline, slide, useTools)
	if err != nil {
		return "", err
	}
	// Keep prompt bounded for very large SVG.
	svg := currentSVG
	const maxSVG = 60000
	if len(svg) > maxSVG {
		svg = svg[:maxSVG] + "\n<!-- truncated -->"
	}
	prompt := base + "\n\n【当前页面 SVG】\n" + svg + "\n\n【用户修改要求】\n" + instruction
	prompt += "\n\n请在尽量保留原页面信息架构与全套 PPT 统一配色（背景明暗/主色/正文色）的前提下，按用户要求修改并输出完整新 SVG。"
	prompt += "\n除非用户明确要求更换整套配色，否则不要把深色页改成浅色页，也不要把浅色页改成深色页。"
	prompt += "\n不要输出解释文字，只输出完整 <svg>...</svg> 或通过工具返回。"
	return prompt, nil
}

// VisualDesignLock is a concrete palette shared by all slides in one deck.
type VisualDesignLock struct {
	Mode          string   `json:"mode"` // dark | light
	Background    string   `json:"background"`
	Surface       string   `json:"surface"`
	PrimaryText   string   `json:"primaryText"`
	SecondaryText string   `json:"secondaryText"`
	Accent        string   `json:"accent"`
	AccentAlt     string   `json:"accentAlt"`
	Border        string   `json:"border"`
	Notes         []string `json:"notes,omitempty"`
}

func (v VisualDesignLock) PromptBlock() string {
	notes := ""
	if len(v.Notes) > 0 {
		notes = "\n补充约束：\n- " + strings.Join(v.Notes, "\n- ")
	}
	return fmt.Sprintf(`背景模式: %s
背景色 background: %s
卡片/面板 surface: %s
主文字 primaryText: %s
次文字 secondaryText: %s
强调色 accent: %s
次强调色 accentAlt: %s
描边/分割线 border: %s
硬性要求:
- 整页背景必须接近 background，不允许改用相反明暗的大面积底色。
- 标题与正文必须使用 primaryText / secondaryText，保证与背景高对比。
- 强调信息、按钮、关键图形使用 accent / accentAlt。
- 卡片、信息块使用 surface，并可用 border 描边。
- 禁止本页单独切换到另一套配色体系。%s`, v.Mode, v.Background, v.Surface, v.PrimaryText, v.SecondaryText, v.Accent, v.AccentAlt, v.Border, notes)
}

// ResolveVisualDesignLock derives a stable palette from free-form style text.
// Important: do NOT over-force a tech-dark theme when user asked for black/white line art.
func ResolveVisualDesignLock(style string) VisualDesignLock {
	raw := strings.TrimSpace(style)
	s := strings.ToLower(raw)

	// 1) Explicit mono / black-white line styles first.
	if containsAny(raw, s, []string{"黑白", "黑白线", "线条", "线稿", "线框", "素描", "mono", "monochrome", "black and white", "b&w", "b/w", "line art", "lineart", "wireframe"}) {
		return monoLineDesignLock(raw)
	}

	// 2) Explicit light / dark keywords.
	lightKeys := []string{"浅色", "亮色", "白底", "白色背景", "浅底", "light", "white background", "明亮", "日间", "极简白"}
	darkKeys := []string{"深色", "暗色", "黑底", "黑色背景", "深底", "dark", "night", "夜间", "赛博", "cyber", "neon"}
	// NOTE: do not treat bare "black"/"黑" alone as dark UI theme; that often means monochrome ink.
	isLight := containsAny(raw, s, lightKeys)
	isDark := containsAny(raw, s, darkKeys)

	// Bare black/white wording without "dark/light theme" => monochrome.
	if !isLight && !isDark && containsAny(raw, s, []string{"黑", "white", "白", "black"}) && containsAny(raw, s, []string{"简约", "简洁", "极简", "simple", "minimal", "minimalist", "线条", "线"}) {
		return monoLineDesignLock(raw)
	}

	if isLight && !isDark {
		return lightDesignLock(raw)
	}
	if isDark && !isLight {
		return darkDesignLock(raw)
	}
	if isLight && isDark {
		// Mixed wording: prefer monochrome if line/simple cues exist.
		if containsAny(raw, s, []string{"线条", "线稿", "简约", "简洁", "极简", "line", "minimal"}) {
			return monoLineDesignLock(raw)
		}
		if containsAny(raw, s, []string{"科技", "tech", "赛博", "cyber", "neon"}) {
			return darkDesignLock(raw)
		}
		return lightDesignLock(raw)
	}

	// 3) No clear theme keyword.
	// Keep lock soft and neutral so we don't override user style semantics.
	if containsAny(raw, s, []string{"科技", "tech", "赛博", "cyber", "neon"}) {
		return darkDesignLock(raw)
	}
	if containsAny(raw, s, []string{"商务", "简报", "academic", "学术", "论文", "报告"}) {
		return lightDesignLock(raw)
	}
	// Default to light minimal, not forced tech-dark.
	return lightDesignLock(raw)
}

func containsAny(raw, lower string, keys []string) bool {
	for _, k := range keys {
		kl := strings.ToLower(k)
		if strings.Contains(lower, kl) || strings.Contains(raw, k) {
			return true
		}
	}
	return false
}

func monoLineDesignLock(style string) VisualDesignLock {
	// Black/white line-art: white paper background + black ink, no colorful accents.
	return VisualDesignLock{
		Mode:          "light",
		Background:    "#FFFFFF",
		Surface:       "#FFFFFF",
		PrimaryText:   "#111111",
		SecondaryText: "#333333",
		Accent:        "#111111",
		AccentAlt:     "#555555",
		Border:        "#111111",
		Notes: []string{
			"黑白线条简约风：背景必须是白/近白，文字与图形以黑色线条为主。",
			"不要使用蓝色/青色/紫色等彩色强调，不要做成深蓝科技深色主题。",
			"优先线框、描边、几何图形与充足留白；少用大面积色块填充。",
			"即使有卡片，也应用白底黑边，而不是深色面板。",
			"用户风格原文: " + strings.TrimSpace(style),
		},
	}
}

func darkDesignLock(style string) VisualDesignLock {
	lock := VisualDesignLock{
		Mode:          "dark",
		Background:    "#0B1220",
		Surface:       "#121A2B",
		PrimaryText:   "#F8FAFC",
		SecondaryText: "#CBD5E1",
		Accent:        "#38BDF8",
		AccentAlt:     "#818CF8",
		Border:        "#334155",
		Notes: []string{
			"深色主题：大面积背景保持深蓝/近黑，不要出现白底整页。",
			"文字以浅色为主；强调色用于少量高亮，不要大面积纯白底卡片。",
			"用户风格原文: " + strings.TrimSpace(style),
		},
	}
	ls := strings.ToLower(style)
	if strings.Contains(style, "绿") || strings.Contains(ls, "green") {
		lock.Accent = "#34D399"
		lock.AccentAlt = "#2DD4BF"
	} else if strings.Contains(style, "橙") || strings.Contains(ls, "orange") {
		lock.Accent = "#FB923C"
		lock.AccentAlt = "#FBBF24"
	} else if strings.Contains(style, "红") || strings.Contains(ls, "red") {
		lock.Accent = "#F87171"
		lock.AccentAlt = "#FB7185"
	} else if strings.Contains(style, "紫") || strings.Contains(ls, "purple") {
		lock.Accent = "#A78BFA"
		lock.AccentAlt = "#C084FC"
	}
	return lock
}

func lightDesignLock(style string) VisualDesignLock {
	lock := VisualDesignLock{
		Mode:          "light",
		Background:    "#F8FAFC",
		Surface:       "#FFFFFF",
		PrimaryText:   "#0F172A",
		SecondaryText: "#334155",
		Accent:        "#2563EB",
		AccentAlt:     "#7C3AED",
		Border:        "#CBD5E1",
		Notes: []string{
			"浅色主题：大面积背景保持浅灰/白，不要出现大面积深色整页背景。",
			"文字以深色为主；强调色用于少量高亮。",
			"用户风格原文: " + strings.TrimSpace(style),
		},
	}
	ls := strings.ToLower(style)
	if strings.Contains(style, "绿") || strings.Contains(ls, "green") {
		lock.Accent = "#059669"
		lock.AccentAlt = "#0D9488"
	} else if strings.Contains(style, "橙") || strings.Contains(ls, "orange") {
		lock.Accent = "#EA580C"
		lock.AccentAlt = "#D97706"
	} else if strings.Contains(style, "红") || strings.Contains(ls, "red") {
		lock.Accent = "#DC2626"
		lock.AccentAlt = "#E11D48"
	} else if strings.Contains(style, "紫") || strings.Contains(ls, "purple") {
		lock.Accent = "#7C3AED"
		lock.AccentAlt = "#9333EA"
	} else if containsAny(style, ls, []string{"简约", "简洁", "极简", "minimal", "minimalist", "线条"}) {
		// Soften colorful accents for minimal styles.
		lock.Accent = "#0F172A"
		lock.AccentAlt = "#475569"
		lock.Notes = append(lock.Notes, "简约风格：减少高饱和彩色，优先黑灰层次与留白。")
	}
	return lock
}

func WithVisualDesignLock(outline PresentationOutline, lock VisualDesignLock) PresentationOutline {
	cp := lock
	outline.VisualDesign = &cp
	return outline
}

func ParseVisualDesignLockJSON(raw string) (VisualDesignLock, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return VisualDesignLock{}, fmt.Errorf("empty theme json")
	}
	// tolerate fenced code blocks
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```JSON")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	// extract first json object
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	var lock VisualDesignLock
	if err := json.Unmarshal([]byte(raw), &lock); err != nil {
		return VisualDesignLock{}, err
	}
	lock = normalizeVisualDesignLock(lock)
	if lock.Background == "" || lock.PrimaryText == "" {
		return VisualDesignLock{}, fmt.Errorf("incomplete theme palette")
	}
	return lock, nil
}

func normalizeVisualDesignLock(lock VisualDesignLock) VisualDesignLock {
	lock.Mode = strings.ToLower(strings.TrimSpace(lock.Mode))
	if lock.Mode != "dark" && lock.Mode != "light" && lock.Mode != "mono" {
		// infer from bg brightness if possible later; keep light default
		if lock.Mode == "" {
			lock.Mode = "light"
		}
	}
	lock.Background = normalizeHexColor(lock.Background)
	lock.Surface = normalizeHexColor(lock.Surface)
	lock.PrimaryText = normalizeHexColor(lock.PrimaryText)
	lock.SecondaryText = normalizeHexColor(lock.SecondaryText)
	lock.Accent = normalizeHexColor(lock.Accent)
	lock.AccentAlt = normalizeHexColor(lock.AccentAlt)
	lock.Border = normalizeHexColor(lock.Border)
	if lock.Surface == "" {
		lock.Surface = lock.Background
	}
	if lock.SecondaryText == "" {
		lock.SecondaryText = lock.PrimaryText
	}
	if lock.Accent == "" {
		lock.Accent = lock.PrimaryText
	}
	if lock.AccentAlt == "" {
		lock.AccentAlt = lock.Accent
	}
	if lock.Border == "" {
		lock.Border = lock.PrimaryText
	}
	return lock
}

func normalizeHexColor(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "#") {
		v = "#" + v
	}
	v = strings.ToUpper(v)
	if len(v) == 4 { // #RGB -> #RRGGBB
		v = "#" + string([]byte{v[1], v[1], v[2], v[2], v[3], v[3]})
	}
	if len(v) != 7 {
		return ""
	}
	for i := 1; i < 7; i++ {
		c := v[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			return ""
		}
	}
	return v
}
