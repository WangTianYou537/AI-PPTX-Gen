package ppt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var fencedJSON = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
var fencedSVG = regexp.MustCompile("(?s)```(?:svg|xml)?\\s*(<svg.*?</svg>)\\s*```")

func ParseOutline(text string) (PresentationOutline, error) {
	var outline PresentationOutline
	if err := json.Unmarshal([]byte(cleanJSONText(text)), &outline); err != nil {
		return PresentationOutline{}, fmt.Errorf("无法解析 PPT 架构 JSON: %w", err)
	}
	if outline.Title == "" || len(outline.Slides) == 0 {
		return PresentationOutline{}, fmt.Errorf("PPT 架构缺少 title 或 slides")
	}
	for i := range outline.Slides {
		if outline.Slides[i].ID == "" {
			outline.Slides[i].ID = fmt.Sprintf("slide-%d", i+1)
		}
	}
	return outline, nil
}

func ParseSVGResponse(text string) (SVGResponse, error) {
	var response SVGResponse
	if err := json.Unmarshal([]byte(cleanJSONText(text)), &response); err != nil {
		return SVGResponse{}, fmt.Errorf("无法解析 SVG JSON: %w", err)
	}
	if len(response.Slides) == 0 {
		return SVGResponse{}, fmt.Errorf("SVG 结果中没有 slides")
	}
	for _, slide := range response.Slides {
		if err := ValidateSVG(slide.SVG); err != nil {
			return SVGResponse{}, fmt.Errorf("%s 的 SVG 不安全或格式不正确: %w", slide.SlideID, err)
		}
	}
	return response, nil
}

func ExtractSVG(text string) (string, error) {
	text = strings.TrimSpace(text)
	if matches := fencedSVG.FindStringSubmatch(text); len(matches) == 2 {
		text = strings.TrimSpace(matches[1])
	}
	start := strings.Index(strings.ToLower(text), "<svg")
	end := strings.LastIndex(strings.ToLower(text), "</svg>")
	if start < 0 || end < start {
		return "", fmt.Errorf("未找到完整 SVG 根元素")
	}
	svg := strings.TrimSpace(text[start : end+len("</svg>")])
	if err := ValidateSVG(svg); err != nil {
		return "", err
	}
	return svg, nil
}

func ValidateSVG(svg string) error {
	trimmed := strings.TrimSpace(strings.ToLower(svg))
	if !strings.HasPrefix(trimmed, "<svg") {
		return fmt.Errorf("必须以 <svg 开头")
	}
	if strings.Contains(trimmed, "<script") || strings.Contains(trimmed, "javascript:") {
		return fmt.Errorf("不能包含脚本")
	}
	return nil
}

func cleanJSONText(text string) string {
	text = strings.TrimSpace(text)
	if matches := fencedJSON.FindStringSubmatch(text); len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}
	startObject := strings.Index(text, "{")
	endObject := strings.LastIndex(text, "}")
	if startObject >= 0 && endObject > startObject {
		return strings.TrimSpace(text[startObject : endObject+1])
	}
	return text
}
