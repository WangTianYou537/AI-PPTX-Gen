package ppt

import (
	"strings"
	"testing"
)

func TestSanitizeUserReportedCorruptedRoots(t *testing.T) {
	samples := []string{
		`<svg viewBox="0 0 1280 720" width="1280" height="720" fill="none" xmlns="[http://www.w3.org/20](http://www.w3.org/20)&lt;svg viewBox=" 0="" 1280="" 720"=""><rect x="0" y="0" width="10" height="10" fill="#FFFFFF"></rect></svg>`,
		`<svg xmlns="[http://www.w3.org/2000](http://www.w3.org/2000)&lt;svg xmlns=" [http:="" www.w3.org="" 2000="" svg](http:="" svg)"="" viewBox="0 0 1280 720" width="1280" height="720"><rect width="1280" height="720" fill="#FFFFFF"></rect></svg>`,
	}
	for i, raw := range samples {
		out, err := SanitizeSVG(raw)
		if err != nil {
			t.Fatalf("sample %d: %v", i+1, err)
		}
		if strings.Contains(out, "[http") || strings.Contains(out, `0=""`) || strings.Contains(out, "svg](") {
			t.Fatalf("sample %d still polluted: %q", i+1, out[:min(180, len(out))])
		}
		if !strings.Contains(out, `xmlns="http://www.w3.org/2000/svg"`) {
			t.Fatalf("sample %d missing xmlns: %q", i+1, out[:min(120, len(out))])
		}
		if !strings.Contains(out, `viewBox="0 0 1280 720"`) {
			t.Fatalf("sample %d missing viewBox: %q", i+1, out[:min(140, len(out))])
		}
	}
}
