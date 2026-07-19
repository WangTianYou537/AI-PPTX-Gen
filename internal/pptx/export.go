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
	keepWorkDir := false
	defer func() {
		if keepWorkDir {
			// debug failure: preserve for inspection
			return
		}
		_ = os.RemoveAll(workDir)
	}()

	// Only export successful slides with SVG content.
	exportable := make([]ppt.SlideSVG, 0, len(slides))
	for _, slide := range slides {
		if strings.TrimSpace(slide.Error) != "" || strings.TrimSpace(slide.SVG) == "" {
			continue
		}
		exportable = append(exportable, slide)
	}
	if len(exportable) == 0 {
		return nil, errors.New("没有可导出的成功页面（存在错误或空 SVG）")
	}

	// Write raw + cleaned SVGs first (helps debug even before officecli runs).
	type preparedSlide struct {
		Slide     ppt.SlideSVG
		RawPath   string
		CleanPath string
		CleanSVG  string
	}
	prepared := make([]preparedSlide, 0, len(exportable))
	var sanitizeErrs []string
	for index, slide := range exportable {
		rawPath := filepath.Join(workDir, fmt.Sprintf("raw-%02d-%s.svg", index+1, safeName(slide.SlideID)))
		_ = os.WriteFile(rawPath, []byte(slide.SVG), 0o600)

		cleanSVG, err := ppt.SanitizeSVG(slide.SVG)
		cleanPath := filepath.Join(workDir, fmt.Sprintf("clean-%02d-%s.svg", index+1, safeName(slide.SlideID)))
		if err != nil {
			sanitizeErrs = append(sanitizeErrs, fmt.Sprintf("%s: %v", slide.SlideID, err))
			_ = os.WriteFile(cleanPath, []byte(slide.SVG), 0o600)
			// still try original if sanitize fails? better skip invalid
			continue
		}
		if err := os.WriteFile(cleanPath, []byte(cleanSVG), 0o600); err != nil {
			return nil, err
		}
		// also write officecli input name mapping
		officePath := filepath.Join(workDir, fmt.Sprintf("slide-%d.svg", index+1))
		if err := os.WriteFile(officePath, []byte(cleanSVG), 0o600); err != nil {
			return nil, err
		}
		prepared = append(prepared, preparedSlide{
			Slide:     slide,
			RawPath:   rawPath,
			CleanPath: cleanPath,
			CleanSVG:  cleanSVG,
		})
	}
	if len(prepared) == 0 {
		if debugEnabled.Load() {
			keepWorkDir = true
			debugDir := preserveExportDebug(workDir, title, "sanitize-failed")
			return nil, fmt.Errorf("所有 SVG 均无法修复为合法 XML: %s（debug 已保留: %s）", strings.Join(sanitizeErrs, "; "), debugDir)
		}
		return nil, fmt.Errorf("所有 SVG 均无法修复为合法 XML: %s", strings.Join(sanitizeErrs, "; "))
	}

	deckPath := filepath.Join(workDir, "deck.pptx")
	if err := runOfficeCLI(ctx, workDir, "create", deckPath); err != nil {
		return failExport(workDir, title, &keepWorkDir, err)
	}

	for index, item := range prepared {
		if err := runOfficeCLI(ctx, workDir, "add", deckPath, "/", "--type", "slide", "--prop", "background=#FFFFFF"); err != nil {
			return failExport(workDir, title, &keepWorkDir, err)
		}
		slidePath := fmt.Sprintf("/slide[%d]", index+1)
		svgPath := filepath.Join(workDir, fmt.Sprintf("slide-%d.svg", index+1))
		if err := runOfficeCLI(ctx, workDir, "add", deckPath, slidePath, "--type", "image", "--prop", "src="+svgPath, "--prop", "x=0", "--prop", "y=0", "--prop", "width="+slideWidth, "--prop", "height="+slideHeight); err != nil {
			return failExport(workDir, title, &keepWorkDir, fmt.Errorf("%s 写入失败: %w", item.Slide.SlideID, err))
		}
	}

	if err := runOfficeCLI(ctx, workDir, "validate", deckPath); err != nil {
		// enrich with local mapping note
		err = fmt.Errorf("%w\n提示: media/imageN.svg 对应导出目录中的 slide-N.svg / raw-N-*.svg / clean-N-*.svg", err)
		return failExport(workDir, title, &keepWorkDir, err)
	}
	content, err := os.ReadFile(deckPath)
	if err != nil {
		return failExport(workDir, title, &keepWorkDir, err)
	}
	if debugEnabled.Load() {
		log.Printf("pptx export deck=%s bytes=%d slides=%d", deckPath, len(content), len(prepared))
	}
	return content, nil
}

func failExport(workDir, title string, keepWorkDir *bool, err error) ([]byte, error) {
	if debugEnabled.Load() {
		*keepWorkDir = true
		debugDir := preserveExportDebug(workDir, title, "export-failed")
		return nil, fmt.Errorf("%w\n(debug 已保留 SVG/导出目录: %s)", err, debugDir)
	}
	return nil, err
}

func preserveExportDebug(workDir, title, reason string) string {
	stamp := time.Now().Format("20060102-150405")
	base := filepath.Join("data", "export-debug")
	_ = os.MkdirAll(base, 0o755)
	name := fmt.Sprintf("%s-%s-%s", stamp, reason, safeName(title))
	dst := filepath.Join(base, name)
	// If rename across devices fails, fall back to copy-ish by leaving workDir and returning it.
	if err := os.Rename(workDir, dst); err != nil {
		log.Printf("pptx export debug preserve rename failed workDir=%s dst=%s err=%v (keeping temp dir)", workDir, dst, err)
		return workDir
	}
	log.Printf("pptx export debug preserved dir=%s", dst)
	// Write an index for convenience.
	_ = os.WriteFile(filepath.Join(dst, "README.txt"), []byte(
		"Export failed while debug enabled.\n"+
			"- raw-NN-<slideId>.svg : original model SVG\n"+
			"- clean-NN-<slideId>.svg : sanitized SVG\n"+
			"- slide-N.svg : file fed to officecli\n"+
			"- deck.pptx : intermediate package (if created)\n",
	), 0o600)
	return dst
}

func safeName(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "deck"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-", " ", "_")
	v = replacer.Replace(v)
	if len(v) > 40 {
		v = v[:40]
	}
	return v
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
		return fmt.Errorf("officecli command timeout: cwd=%s command=%s", dir, shellCommand("officecli", args...))
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
