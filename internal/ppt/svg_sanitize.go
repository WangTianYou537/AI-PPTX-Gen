package ppt

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// tag openers for rewriting attributes safely
	tagOpen  = regexp.MustCompile(`(?s)<(/)?([A-Za-z_:][\w:.\-]*)([^>]*)>`)
	numToken = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
)

// SanitizeSVG repairs common LLM-generated SVG XML issues that break PPTX packaging.
func SanitizeSVG(svg string) (string, error) {
	svg = strings.TrimSpace(svg)
	if svg == "" {
		return "", fmt.Errorf("SVG 为空")
	}
	// Strip UTF-8 BOM.
	svg = strings.TrimPrefix(svg, string([]byte{0xEF, 0xBB, 0xBF}))
	// Keep from first <svg ...>
	if i := strings.Index(strings.ToLower(svg), "<svg"); i > 0 {
		svg = svg[i:]
	}
	// Drop trailing junk after last </svg>
	if j := strings.LastIndex(strings.ToLower(svg), "</svg>"); j >= 0 {
		svg = svg[:j+len("</svg>")]
	}

	// Repair corrupted <svg ...> root attributes (markdown/link pollution of xmlns).
	svg = rebuildRootSVG(svg)

	svg = stripIllegalXMLChars(svg)
	svg = escapeBareAmpersands(svg)
	svg = sanitizeTags(svg)

	if err := ValidateSVG(svg); err != nil {
		return "", err
	}
	if err := quickXMLWellFormedCheck(svg); err != nil {
		return "", err
	}
	return svg, nil
}

func sanitizeTags(svg string) string {
	return tagOpen.ReplaceAllStringFunc(svg, func(m string) string {
		sub := tagOpen.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		closing := sub[1]
		name := sub[2]
		attrs := strings.TrimSpace(sub[3])
		selfClose := false
		if strings.HasSuffix(attrs, "/") {
			selfClose = true
			attrs = strings.TrimSpace(strings.TrimSuffix(attrs, "/"))
		}
		if closing == "/" {
			return "</" + name + ">"
		}
		cleanAttrs := sanitizeAttrBlock(attrs)
		if selfClose {
			if cleanAttrs == "" {
				return "<" + name + " />"
			}
			return "<" + name + " " + cleanAttrs + " />"
		}
		if cleanAttrs == "" {
			return "<" + name + ">"
		}
		return "<" + name + " " + cleanAttrs + ">"
	})
}

func sanitizeAttrBlock(attrs string) string {
	if attrs == "" {
		return ""
	}
	var parts []string
	i := 0
	for i < len(attrs) {
		for i < len(attrs) && unicode.IsSpace(rune(attrs[i])) {
			i++
		}
		if i >= len(attrs) {
			break
		}
		start := i
		for i < len(attrs) && !unicode.IsSpace(rune(attrs[i])) && attrs[i] != '=' {
			i++
		}
		name := attrs[start:i]
		for i < len(attrs) && unicode.IsSpace(rune(attrs[i])) {
			i++
		}
		if i >= len(attrs) || attrs[i] != '=' {
			if isValidXMLName(name) {
				parts = append(parts, name+`="true"`)
			}
			continue
		}
		i++ // =
		for i < len(attrs) && unicode.IsSpace(rune(attrs[i])) {
			i++
		}
		if i >= len(attrs) {
			break
		}
		var val string
		if attrs[i] == '"' || attrs[i] == '\'' {
			q := attrs[i]
			i++
			vstart := i
			for i < len(attrs) && attrs[i] != q {
				i++
			}
			val = attrs[vstart:i]
			if i < len(attrs) && attrs[i] == q {
				i++
			}
		} else {
			vstart := i
			for i < len(attrs) && !unicode.IsSpace(rune(attrs[i])) {
				i++
			}
			val = attrs[vstart:i]
		}
		if !isValidXMLName(name) {
			continue
		}
		parts = append(parts, name+`="`+escapeXMLAttr(val)+`"`)
	}
	return strings.Join(parts, " ")
}

func isValidXMLName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	r, size := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError && size == 1 {
		return false
	}
	if !(unicode.IsLetter(r) || r == '_' || r == ':') {
		return false
	}
	for _, rr := range name[size:] {
		if unicode.IsLetter(rr) || unicode.IsDigit(rr) || rr == '.' || rr == '-' || rr == '_' || rr == ':' {
			continue
		}
		return false
	}
	return true
}

func rebuildRootSVG(svg string) string {
	lower := strings.ToLower(svg)
	start := strings.Index(lower, "<svg")
	if start < 0 {
		return svg
	}
	// Corrupted roots often break quote tracking; use first '>' after <svg.
	relEnd := strings.Index(svg[start:], ">")
	if relEnd < 0 {
		return svg
	}
	end := start + relEnd
	open := svg[start : end+1]
	rest := svg[end+1:]
	if !rootLooksPolluted(open) {
		return svg
	}

	width, height, viewBox, fillNone := extractRootGeometry(open)
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"`)
	b.WriteString(` viewBox="` + escapeXMLAttr(viewBox) + `"`)
	b.WriteString(` width="` + escapeXMLAttr(width) + `"`)
	b.WriteString(` height="` + escapeXMLAttr(height) + `"`)
	if fillNone {
		b.WriteString(` fill="none"`)
	}
	if strings.HasSuffix(strings.TrimSpace(open), "/>") {
		b.WriteString(" />")
		return b.String()
	}
	b.WriteString(">")
	return b.String() + rest
}

func rootLooksPolluted(open string) bool {
	lo := strings.ToLower(open)
	if strings.Contains(lo, "[http") || strings.Contains(lo, "](") || strings.Contains(open, "&lt;svg") {
		return true
	}
	if strings.Count(lo, "<svg") > 1 {
		return true
	}
	// common leftovers after xmlns pollution
	if strings.Contains(open, `0=""`) || strings.Contains(open, `1280=""`) || strings.Contains(open, `720=""`) || strings.Contains(open, `2000=""`) {
		return true
	}
	if strings.Contains(lo, "xmlns=") {
		if strings.Contains(lo, "xmlns=\"[") || strings.Contains(lo, "xmlns='[") {
			return true
		}
		// truncated/mangled w3 namespace
		if strings.Contains(lo, "www.w3.org") && !strings.Contains(lo, "www.w3.org/2000/svg") {
			return true
		}
	}
	return false
}

func extractRootGeometry(openTag string) (width, height, viewBox string, fillNone bool) {
	width = firstAttr(openTag, "width")
	height = firstAttr(openTag, "height")
	viewBox = firstAttr(openTag, "viewBox")
	if viewBox == "" {
		viewBox = firstAttr(openTag, "viewbox")
	}
	if !looksLikeViewBox(viewBox) {
		if vb := recoverViewBoxFromMangled(openTag); vb != "" {
			viewBox = vb
		}
	}
	if !looksLikeViewBox(viewBox) {
		viewBox = "0 0 1280 720"
	}
	if strings.TrimSpace(width) == "" || !isNumericLike(width) {
		fields := strings.Fields(viewBox)
		if len(fields) == 4 {
			width = fields[2]
		} else {
			width = "1280"
		}
	}
	if strings.TrimSpace(height) == "" || !isNumericLike(height) {
		fields := strings.Fields(viewBox)
		if len(fields) == 4 {
			height = fields[3]
		} else {
			height = "720"
		}
	}
	lo := strings.ToLower(openTag)
	fillNone = strings.Contains(lo, `fill="none"`) || strings.Contains(lo, "fill='none'") || strings.Contains(lo, "fill=none")
	return width, height, viewBox, fillNone
}

func firstAttr(openTag, name string) string {
	lower := strings.ToLower(openTag)
	for _, q := range []string{`"`, "'"} {
		key := strings.ToLower(name) + "=" + q
		if i := strings.Index(lower, key); i >= 0 {
			i += len(key)
			j := strings.Index(openTag[i:], q)
			if j >= 0 {
				return strings.TrimSpace(openTag[i : i+j])
			}
		}
	}
	return ""
}

func looksLikeViewBox(v string) bool {
	fields := strings.Fields(strings.TrimSpace(v))
	if len(fields) != 4 {
		return false
	}
	for _, f := range fields {
		if !isNumericLike(f) {
			return false
		}
	}
	return true
}

func isNumericLike(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' {
			continue
		}
		// unit suffix allowed after digits
		return i > 0
	}
	return true
}

func recoverViewBoxFromMangled(openTag string) string {
	lower := strings.ToLower(openTag)
	segment := openTag
	if idx := strings.Index(lower, "viewbox"); idx >= 0 {
		segment = openTag[idx:]
		if len(segment) > 160 {
			segment = segment[:160]
		}
	}
	nums := numToken.FindAllString(segment, 8)
	if len(nums) >= 4 {
		return strings.Join(nums[:4], " ")
	}
	nums = numToken.FindAllString(openTag, 8)
	if len(nums) >= 4 {
		return strings.Join(nums[:4], " ")
	}
	return ""
}

func stripIllegalXMLChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 0x9 || r == 0xA || r == 0xD || r >= 0x20 {
			if r == 0xFFFE || r == 0xFFFF {
				continue
			}
			b.WriteRune(r)
		}
	}
	out := b.String()
	if !utf8.ValidString(out) {
		return strings.ToValidUTF8(out, "")
	}
	return out
}

func escapeBareAmpersands(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		j := i + 1
		if j < len(s) && s[j] == '#' {
			j++
			if j < len(s) && (s[j] == 'x' || s[j] == 'X') {
				j++
				for j < len(s) && isHex(s[j]) {
					j++
				}
			} else {
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
			}
			if j < len(s) && s[j] == ';' && j > i+2 {
				b.WriteString(s[i : j+1])
				i = j
				continue
			}
		} else {
			k := j
			for k < len(s) && isNameChar(s[k]) {
				k++
			}
			if k < len(s) && s[k] == ';' && k > i+1 {
				b.WriteString(s[i : k+1])
				i = k
				continue
			}
		}
		b.WriteString("&amp;")
	}
	return b.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' || c == ':'
}

func escapeXMLAttr(v string) string {
	v = escapeBareAmpersands(v)
	replacer := strings.NewReplacer(
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(v)
}

func quickXMLWellFormedCheck(svg string) error {
	inAttr := byte(0)
	for i := 0; i < len(svg); i++ {
		c := svg[i]
		if inAttr != 0 {
			if c == inAttr {
				inAttr = 0
				continue
			}
			if c == '<' {
				ctx := svg[max(0, i-40):min(len(svg), i+20)]
				return fmt.Errorf("SVG 属性中包含未转义的 '<'（附近: %q）", compact(ctx))
			}
			continue
		}
		if c == '"' || c == '\'' {
			j := i - 1
			for j >= 0 && (svg[j] == ' ' || svg[j] == '\t' || svg[j] == '\n' || svg[j] == '\r') {
				j--
			}
			if j >= 0 && svg[j] == '=' {
				inAttr = c
			}
		}
	}
	if inAttr != 0 {
		return fmt.Errorf("SVG 属性引号未闭合")
	}
	return nil
}

func compact(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
