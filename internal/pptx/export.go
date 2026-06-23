package pptx

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"wty5.cn/ppt-gen/internal/ppt"
)

const (
	slideWidth  = "13.333333in"
	slideHeight = "7.5in"
)

var debugEnabled atomic.Bool

func SetDebug(enabled bool) {
	debugEnabled.Store(enabled)
}

func ExportFromSVGSlides(ctx context.Context, title string, slides []ppt.SlideSVG) ([]byte, error) {
	if len(slides) == 0 {
		return nil, errors.New("没有可导出的 SVG 页面")
	}
	if _, err := exec.LookPath("officecli"); err != nil {
		return nil, errors.New("officecli 未安装或不在 PATH，请先安装 officecli")
	}

	workDir, err := os.MkdirTemp("", "ppt-gen-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	deckPath := filepath.Join(workDir, "deck.pptx")
	if err := runOfficeCLI(ctx, workDir, "create", deckPath); err != nil {
		return nil, err
	}

	for index, slide := range slides {
		if err := ppt.ValidateSVG(slide.SVG); err != nil {
			return nil, fmt.Errorf("%s 的 SVG 不安全或格式不正确: %w", slide.SlideID, err)
		}
		svgPath := filepath.Join(workDir, fmt.Sprintf("slide-%d.svg", index+1))
		if err := os.WriteFile(svgPath, []byte(slide.SVG), 0o600); err != nil {
			return nil, err
		}
		if err := runOfficeCLI(ctx, workDir, "add", deckPath, "/", "--type", "slide", "--prop", "background=#FFFFFF"); err != nil {
			return nil, err
		}
		slidePath := fmt.Sprintf("/slide[%d]", index+1)
		if err := runOfficeCLI(ctx, workDir, "add", deckPath, slidePath, "--type", "image", "--prop", "src="+svgPath, "--prop", "x=0", "--prop", "y=0", "--prop", "width="+slideWidth, "--prop", "height="+slideHeight); err != nil {
			return nil, err
		}
	}

	_ = runOfficeCLI(ctx, workDir, "validate", deckPath)
	return os.ReadFile(deckPath)
}

func runOfficeCLI(ctx context.Context, dir string, args ...string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if debugEnabled.Load() {
		log.Printf("officecli cwd=%s command=%s", dir, shellCommand("officecli", args...))
	}
	cmd := exec.CommandContext(cmdCtx, "officecli", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if debugEnabled.Load() && len(output) > 0 {
		log.Printf("officecli output=%s", strings.TrimSpace(string(output)))
	}
	if cmdCtx.Err() == context.DeadlineExceeded {
		return errors.New("officecli 执行超时")
	}
	if err != nil {
		message := string(output)
		if len(message) > 1200 {
			message = message[:1200] + "..."
		}
		return fmt.Errorf("officecli %v 执行失败: %w\n%s", args, err, message)
	}
	return nil
}

func shellCommand(name string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(name))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
