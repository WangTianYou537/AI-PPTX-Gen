package llm

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestGeminiPartsFromMessageInlinePDF(t *testing.T) {
	raw := []byte("%PDF-1.4 demo")
	msg := Message{Role: "user", Parts: []ContentPart{
		{Type: "text", Text: "Summarize this document"},
		{Type: "file", MIMEType: "application/pdf", Data: raw, Filename: "demo.pdf"},
	}}
	parts := geminiPartsFromMessage(msg)
	if len(parts) != 2 {
		t.Fatalf("parts=%d", len(parts))
	}
	if parts[0].Text != "Summarize this document" {
		t.Fatalf("text=%q", parts[0].Text)
	}
	if parts[1].InlineData == nil {
		t.Fatal("missing inline_data")
	}
	if parts[1].InlineData.MIMEType != "application/pdf" {
		t.Fatalf("mime=%s", parts[1].InlineData.MIMEType)
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1].InlineData.Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(raw) {
		t.Fatalf("decoded mismatch: %q", decoded)
	}
}

func TestOpenAIContentFromMessagePDF(t *testing.T) {
	raw := []byte("%PDF-1.4 demo")
	msg := Message{Role: "user", Parts: []ContentPart{
		{Type: "text", Text: "Summarize"},
		{Type: "file", MIMEType: "application/pdf", Data: raw, Filename: "demo.pdf"},
	}}
	content := openAIContentFromMessage(msg)
	b, _ := json.Marshal(content)
	s := string(b)
	if !strings.Contains(s, "file_data") || !strings.Contains(s, "application/pdf") {
		t.Fatalf("unexpected content: %s", s)
	}
	// Should not double-send PDF as image_url.
	if strings.Count(s, "image_url") != 0 {
		t.Fatalf("pdf should not use image_url: %s", s)
	}
}
