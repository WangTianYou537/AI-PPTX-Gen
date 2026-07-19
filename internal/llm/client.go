package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const llmHTTPTimeout = 10 * time.Minute

var defaultHTTPClient = &http.Client{Timeout: llmHTTPTimeout}
var debugEnabled atomic.Bool

func httpClientFor(proxy string) (*http.Client, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return defaultHTTPClient, nil
	}
	u, err := url.Parse(proxy)
	if err != nil {
		return nil, NewUserError("Proxy 地址无效: " + err.Error())
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(u),
	}
	return &http.Client{Timeout: llmHTTPTimeout, Transport: transport}, nil
}

func SetDebug(enabled bool) {
	debugEnabled.Store(enabled)
}

func Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	if err := req.Config.Validate(); err != nil {
		return GenerateResponse{}, err
	}

	switch req.Config.Provider {
	case ProviderOpenAI:
		return generateOpenAI(ctx, req)
	case ProviderOpenAIResponses:
		return generateOpenAIResponses(ctx, req)
	case ProviderGemini:
		return generateGemini(ctx, req)
	case ProviderClaude:
		return generateClaude(ctx, req)
	default:
		return GenerateResponse{}, NewUserError("暂不支持该模型供应商")
	}
}

func postJSON(ctx context.Context, url string, headers map[string]string, payload any, proxy string) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	if debugEnabled.Load() {
		log.Printf("llm request method=POST url=%s headers=%s content_length=%d payload=%s", url, debugHeaderMap(headers), len(body), debugSnippet(string(body)))
	}

	client, err := httpClientFor(proxy)
	if err != nil {
		return nil, 0, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}

	started := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		if debugEnabled.Load() {
			log.Printf("llm request failed method=POST url=%s duration=%s timeout=%s err=%v", url, time.Since(started), llmHTTPTimeout, err)
			log.Printf("llm debug curl for failed request:\n%s", debugCurlCommand(url, headers, body))
		}
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		if debugEnabled.Load() {
			log.Printf("llm response read failed method=POST url=%s status=%d duration=%s timeout=%s err=%v", url, resp.StatusCode, time.Since(started), llmHTTPTimeout, err)
			log.Printf("llm debug curl for failed response read:\n%s", debugCurlCommand(url, headers, body))
		}
		return nil, resp.StatusCode, err
	}
	if debugEnabled.Load() {
		log.Printf("llm response status=%d status_text=%q duration=%s content_length=%d headers=%s body=%s", resp.StatusCode, resp.Status, time.Since(started), len(respBody), debugResponseHeaders(resp.Header), debugSnippet(string(respBody)))
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("llm debug curl for non-2xx:\n%s", debugCurlCommand(url, headers, body))
		}
	}
	return respBody, resp.StatusCode, nil
}

func postJSONStream(ctx context.Context, url string, headers map[string]string, payload any, proxy string) (GenerateResponse, int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return GenerateResponse{}, 0, nil, err
	}
	if debugEnabled.Load() {
		log.Printf("llm stream request method=POST url=%s headers=%s content_length=%d payload=%s", url, debugHeaderMap(headers), len(body), debugSnippet(string(body)))
	}

	client, err := httpClientFor(proxy)
	if err != nil {
		return GenerateResponse{}, 0, nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return GenerateResponse{}, 0, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}

	started := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		if debugEnabled.Load() {
			log.Printf("llm stream request failed method=POST url=%s duration=%s timeout=%s err=%v", url, time.Since(started), llmHTTPTimeout, err)
			log.Printf("llm debug curl for failed stream request:\n%s", debugCurlCommand(url, headers, body))
		}
		return GenerateResponse{}, 0, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			if debugEnabled.Load() {
				log.Printf("llm stream error response read failed method=POST url=%s status=%d duration=%s timeout=%s err=%v", url, resp.StatusCode, time.Since(started), llmHTTPTimeout, err)
				log.Printf("llm debug curl for failed stream error response read:\n%s", debugCurlCommand(url, headers, body))
			}
			return GenerateResponse{}, resp.StatusCode, nil, err
		}
		if debugEnabled.Load() {
			log.Printf("llm stream response status=%d status_text=%q duration=%s content_length=%d headers=%s body=%s", resp.StatusCode, resp.Status, time.Since(started), len(respBody), debugResponseHeaders(resp.Header), debugSnippet(string(respBody)))
			log.Printf("llm debug curl for non-2xx:\n%s", debugCurlCommand(url, headers, body))
		}
		return GenerateResponse{}, resp.StatusCode, respBody, nil
	}

	response, raw, err := readOpenAIStream(resp.Body)
	if debugEnabled.Load() {
		log.Printf("llm stream response status=%d status_text=%q duration=%s content_length=%d headers=%s body=%s", resp.StatusCode, resp.Status, time.Since(started), len(raw), debugResponseHeaders(resp.Header), debugSnippet(string(raw)))
	}
	if err != nil {
		if debugEnabled.Load() {
			log.Printf("llm stream read failed method=POST url=%s status=%d duration=%s timeout=%s err=%v", url, resp.StatusCode, time.Since(started), llmHTTPTimeout, err)
			log.Printf("llm debug curl for failed stream read:\n%s", debugCurlCommand(url, headers, body))
		}
		// Keep raw SSE body so provider-specific parsers (e.g. OpenAI Responses) can recover.
		return GenerateResponse{}, resp.StatusCode, raw, err
	}
	return response, resp.StatusCode, raw, nil
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   any `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		Message struct {
			Content   any `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		Text string `json:"text"`
	} `json:"choices"`
	// Some proxies emit top-level text fields in SSE data frames.
	Text    string `json:"text"`
	Content any    `json:"content"`
}

type streamToolCall struct {
	Name      string
	Arguments strings.Builder
}

func readOpenAIStream(body io.Reader) (GenerateResponse, []byte, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var text strings.Builder
	var raw strings.Builder
	toolCalls := map[int]*streamToolCall{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		raw.WriteString(line)
		raw.WriteByte('\n')
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return GenerateResponse{Text: text.String()}, []byte(raw.String()), fmt.Errorf("解析 OpenAI 流式响应失败: %w", err)
		}
		text.WriteString(anyToString(chunk.Content))
		text.WriteString(chunk.Text)
		for _, choice := range chunk.Choices {
			text.WriteString(anyToString(choice.Delta.Content))
			text.WriteString(anyToString(choice.Message.Content))
			text.WriteString(choice.Text)
			for _, call := range choice.Delta.ToolCalls {
				toolCall := toolCalls[call.Index]
				if toolCall == nil {
					toolCall = &streamToolCall{}
					toolCalls[call.Index] = toolCall
				}
				if call.Function.Name != "" {
					toolCall.Name = call.Function.Name
				}
				toolCall.Arguments.WriteString(call.Function.Arguments)
			}
			for index, call := range choice.Message.ToolCalls {
				toolCall := toolCalls[index]
				if toolCall == nil {
					toolCall = &streamToolCall{}
					toolCalls[index] = toolCall
				}
				if call.Function.Name != "" {
					toolCall.Name = call.Function.Name
				}
				toolCall.Arguments.WriteString(call.Function.Arguments)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return GenerateResponse{Text: text.String()}, []byte(raw.String()), err
	}
	for index := 0; ; index++ {
		toolCall := toolCalls[index]
		if toolCall == nil {
			break
		}
		if toolCall.Name != "" {
			return GenerateResponse{Text: text.String(), ToolName: toolCall.Name, ToolInput: json.RawMessage(toolCall.Arguments.String())}, []byte(raw.String()), nil
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		// Non-SSE JSON body from some OpenAI-compatible proxies.
		if recovered, err := parseOpenAIChatCompletionFlexible([]byte(raw.String())); err == nil && (recovered.Text != "" || recovered.ToolName != "") {
			return recovered, []byte(raw.String()), nil
		}
		// Also try stripping "data:" prefixes and concatenating payloads.
		var joined strings.Builder
		for _, line := range strings.Split(raw.String(), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				joined.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if joined.Len() > 0 {
			if recovered, err := parseOpenAIChatCompletionFlexible([]byte(joined.String())); err == nil && (recovered.Text != "" || recovered.ToolName != "") {
				return recovered, []byte(raw.String()), nil
			}
		}
	}
	return GenerateResponse{Text: text.String()}, []byte(raw.String()), nil
}

func debugCurlCommand(requestURL string, headers map[string]string, body []byte) string {
	var builder strings.Builder
	builder.WriteString("curl -i -sS -X POST")
	builder.WriteString(" ")
	builder.WriteString(shellQuote(requestURL))
	builder.WriteString(" \\\n  -H ")
	builder.WriteString(shellQuote("Content-Type: application/json"))
	for key, value := range headers {
		builder.WriteString(" \\\n  -H ")
		builder.WriteString(shellQuote(key + ": " + value))
	}
	builder.WriteString(" \\\n  --data-binary ")
	builder.WriteString(shellQuote(string(body)))
	return builder.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func debugSnippet(text string) string {
	const limit = 4000
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "...<truncated>"
}

func debugHeaderMap(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	payload, err := json.Marshal(headers)
	if err != nil {
		return fmt.Sprintf("%v", headers)
	}
	return string(payload)
}

func debugResponseHeaders(headers http.Header) string {
	selected := map[string][]string{}
	interesting := []string{
		"content-type",
		"content-length",
		"x-request-id",
		"request-id",
		"x-trace-id",
		"x-ratelimit-limit-requests",
		"x-ratelimit-remaining-requests",
		"x-ratelimit-reset-requests",
		"retry-after",
		"server",
		"cf-ray",
	}
	for _, key := range interesting {
		if values, ok := headers[http.CanonicalHeaderKey(key)]; ok {
			selected[http.CanonicalHeaderKey(key)] = values
		}
	}
	payload, err := json.Marshal(selected)
	if err != nil {
		return fmt.Sprintf("%v", selected)
	}
	return string(payload)
}

func firstTextBlock(blocks []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) (string, error) {
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("模型响应中没有文本内容")
}
