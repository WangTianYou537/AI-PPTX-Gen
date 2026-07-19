package ppt

import (
	"strings"
	"testing"
)

func TestSanitizeSVGRepairsPollutedXMLNS(t *testing.T) {
	raw := `<svg viewBox="0 0 1280 720" width="1280" height="720" fill="none" xmlns="[http://www.w3.org/20](http://www.w3.org/20)&lt;svg viewBox=" 0="" 1280="" 720"=""><rect x="0" y="0" width="10" height="10" fill="#FFFFFF"></rect></svg>`
	out, err := SanitizeSVG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `xmlns="http://www.w3.org/2000/svg"`) {
		t.Fatalf("xmlns not repaired: %q", out[:min(len(out), 160)])
	}
	if strings.Contains(out, `0=""`) || strings.Contains(out, `[http:`) || strings.Contains(out, `720"=""`) {
		t.Fatalf("polluted attrs remain: %q", out[:min(len(out), 220)])
	}
	if !strings.Contains(out, `viewBox="0 0 1280 720"`) {
		t.Fatalf("viewBox missing/wrong: %q", out[:min(len(out), 160)])
	}
}

func TestSanitizeSVGRepairsNestedSVGInXMLNS(t *testing.T) {
	raw := `<svg xmlns="[http://www.w3.org/2000](http://www.w3.org/2000)&lt;svg xmlns=" [http:="" www.w3.org="" 2000="" svg](http:="" svg)"="" viewBox="0 0 1280 720" width="1280" height="720"><rect width="1280" height="720" fill="#FFFFFF"></rect></svg>`
	out, err := SanitizeSVG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[http:") || strings.Contains(out, `svg](`) || strings.Contains(out, `2000=""`) {
		t.Fatalf("nested pollution remains: %q", out[:min(len(out), 220)])
	}
	if !strings.HasPrefix(strings.TrimSpace(out), `<svg xmlns="http://www.w3.org/2000/svg"`) {
		t.Fatalf("root not normalized: %q", out[:min(len(out), 120)])
	}
}
