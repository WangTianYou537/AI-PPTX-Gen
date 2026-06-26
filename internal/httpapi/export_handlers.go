package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/ppt"
	"wty5.cn/ppt-gen/internal/pptx"
)

type exportPPTXRequest struct {
	Title  string         `json:"title"`
	Slides []ppt.SlideSVG `json:"slides"`
}

func (s *Server) handleExportPPTX(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input exportPPTXRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if len(input.Slides) == 0 {
		writeError(w, http.StatusBadRequest, "没有可导出的 SVG 页面")
		return
	}

	content, err := pptx.ExportFromSVGSlides(r.Context(), input.Title, input.Slides)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	filename := safePPTXFilename(input.Title)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(content); err != nil {
		if debugEnabled.Load() {
			writeError(w, http.StatusInternalServerError, "写入 PPTX 响应失败: "+err.Error())
		}
	}
}

func safePPTXFilename(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "ppt-gen.pptx"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	title = replacer.Replace(title)
	if len(title) > 80 {
		title = title[:80]
	}
	return title + ".pptx"
}
