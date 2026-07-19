package ppt

import (
	"strings"
	"testing"
)

func TestResolveVisualDesignLockDark(t *testing.T) {
	lock := ResolveVisualDesignLock("科技、深色、简洁")
	if lock.Mode != "dark" {
		t.Fatalf("mode=%s", lock.Mode)
	}
}

func TestResolveVisualDesignLockLight(t *testing.T) {
	lock := ResolveVisualDesignLock("浅色、白底、商务")
	if lock.Mode != "light" {
		t.Fatalf("mode=%s", lock.Mode)
	}
}

func TestResolveVisualDesignLockMonoLine(t *testing.T) {
	lock := ResolveVisualDesignLock("黑白线条简约风")
	if lock.Mode != "light" {
		t.Fatalf("mode=%s", lock.Mode)
	}
	if lock.Background != "#FFFFFF" || lock.PrimaryText != "#111111" {
		t.Fatalf("unexpected mono palette: %+v", lock)
	}
	// Must not become blue tech palette.
	if lock.Accent == "#38BDF8" || lock.Background == "#0B1220" {
		t.Fatalf("mono style incorrectly mapped to tech dark: %+v", lock)
	}
	joined := strings.Join(lock.Notes, " ")
	if !strings.Contains(joined, "黑白") && !strings.Contains(joined, "线条") {
		t.Fatalf("missing mono notes: %v", lock.Notes)
	}
}

func TestBuildSlideSVGPromptRespectsUserStyle(t *testing.T) {
	outline := PresentationOutline{
		Title:    "Demo",
		Theme:    "AI",
		Audience: "学生",
		Style:    "黑白线条简约风",
		Slides: []SlideOutline{{
			ID: "slide-1", Title: "封面", Purpose: "开场", KeyPoints: []string{"a"}, VisualHint: "居中标题",
		}},
	}
	prompt, err := BuildSlideSVGPrompt(outline, outline.Slides[0], false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "风格优先级") {
		t.Fatalf("missing style priority guidance")
	}
	if !strings.Contains(prompt, "黑白线条") && !strings.Contains(prompt, "#FFFFFF") {
		t.Fatalf("prompt does not preserve mono style: %s", prompt)
	}
	if strings.Contains(prompt, "#0B1220") {
		t.Fatalf("mono style still forced to dark tech bg")
	}
}
