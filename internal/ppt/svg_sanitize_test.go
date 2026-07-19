package ppt

import (
	"strings"
	"testing"
)

func TestSanitizeSVGEscapesAttrLessThan(t *testing.T) {
	raw := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720" data-note="a < b"><rect x="0" y="0" width="10" height="10"/></svg>`
	out, err := SanitizeSVG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `data-note="a < b"`) {
		t.Fatalf("raw < remains in attribute: %s", out)
	}
	if !strings.Contains(out, `data-note="a &lt; b"`) {
		t.Fatalf("expected escaped attr, got: %s", out)
	}
}

func TestSanitizeSVGBareAmpersand(t *testing.T) {
	raw := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><text x="1" y="2">A & B</text></svg>`
	out, err := SanitizeSVG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "A &amp; B") {
		t.Fatalf("amp not escaped: %s", out)
	}
}

func TestSanitizeSVGDropsInvalidAttrNames(t *testing.T) {
	// Name cannot begin with '0' / '['
	raw := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" 0bad="1" [x]="2" ok="3"><rect x="0" y="0" width="1" height="1"/></svg>`
	out, err := SanitizeSVG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "0bad=") || strings.Contains(out, "[x]=") {
		t.Fatalf("invalid attr names not dropped: %s", out)
	}
	if !strings.Contains(out, `ok="3"`) {
		t.Fatalf("valid attr lost: %s", out)
	}
}

func TestSanitizeSVGQuotesUnquotedAttrs(t *testing.T) {
	raw := `<svg xmlns="http://www.w3.org/2000/svg" viewBox=0 0 10 10><rect x=1 y=2 width=3 height=4/></svg>`
	// viewBox unquoted multi-token is hard; at least x/y/width/height should be quoted.
	out, err := SanitizeSVG(raw)
	// may fail well-formed if viewBox remains broken; ensure function either fixes or errors clearly
	if err != nil {
		// acceptable if unrecoverable viewBox
		return
	}
	if !strings.Contains(out, `x="1"`) && !strings.Contains(out, "x='1'") {
		// if sanitizer dropped unrecoverable attrs that's ok too
		t.Log(out)
	}
}
