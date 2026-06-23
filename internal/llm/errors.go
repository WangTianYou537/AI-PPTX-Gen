package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

type UserError struct {
	Message string
}

func NewUserError(message string) UserError {
	return UserError{Message: message}
}

func (e UserError) Error() string {
	return e.Message
}

func providerError(provider Provider, status int, body string) error {
	message := extractProviderMessage(body)
	if len(message) > 1200 {
		message = message[:1200] + "..."
	}
	if status == httpStatusServiceUnavailable {
		return fmt.Errorf("%s API 暂时不可用或上游服务繁忙（503）：%s。请稍后重试，或在后台为该生成角色切换模型/Base URL", provider, message)
	}
	return fmt.Errorf("%s API 返回错误 %d: %s", provider, status, message)
}

const httpStatusServiceUnavailable = 503

func extractProviderMessage(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "响应为空"
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return body
	}
	if msg := stringField(payload, "message"); msg != "" {
		return msg
	}
	if msg := stringField(payload, "error"); msg != "" {
		return msg
	}
	if code := stringField(payload, "code"); code != "" {
		return "error code: " + code
	}
	if errorValue, ok := payload["error"].(map[string]any); ok {
		if msg := stringField(errorValue, "message"); msg != "" {
			if code := stringField(errorValue, "code"); code != "" {
				return msg + " (code: " + code + ")"
			}
			return msg
		}
		if code := stringField(errorValue, "code"); code != "" {
			return "error code: " + code
		}
	}
	return body
}

func stringField(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return ""
	}
}
