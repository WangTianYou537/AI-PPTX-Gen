package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"wty5.cn/ppt-gen/internal/domain"
	"wty5.cn/ppt-gen/internal/store"
)

func validateRoleSettings(label string, settings store.GenerationRoleSettings) error {
	if err := validatePrompt(settings.SystemPrompt); err != nil {
		return errors.New(label + "提示词" + err.Error())
	}
	if err := domain.ValidateRequestJSON(settings.RequestJSON); err != nil {
		return errors.New(label + err.Error())
	}
	if strings.TrimSpace(settings.ProviderID) == "" {
		return errors.New(label + "请选择 LLM 提供商")
	}
	if strings.TrimSpace(settings.Model) == "" {
		return errors.New(label + "请选择模型")
	}
	return nil
}

func validatePrompt(prompt string) error {
	length := len(strings.TrimSpace(prompt))
	if length == 0 {
		return errors.New("不能为空")
	}
	if length > 20000 {
		return errors.New("不能超过 20000 字符")
	}
	return nil
}

func handleAdminStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "资源不存在")
	case errors.Is(err, store.ErrAlreadyExists):
		writeError(w, http.StatusBadRequest, "邮箱已存在")
	case errors.Is(err, store.ErrInvalidStore):
		writeError(w, http.StatusBadRequest, "操作不符合当前数据约束")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func applyNullableLimit(raw map[string]json.RawMessage, key string, target **int, clear *bool) error {
	value, ok := raw[key]
	if !ok {
		return nil
	}
	if string(value) == "null" {
		*clear = true
		return nil
	}
	var parsed int
	if err := json.Unmarshal(value, &parsed); err != nil {
		return errors.New("额度必须是数字或 null")
	}
	*target = &parsed
	return nil
}

func validateLimits(values ...*int) error {
	for _, value := range values {
		if value != nil && *value < 0 {
			return errors.New("额度不能小于 0")
		}
	}
	return nil
}

func validateConcurrencyLimit(value *int) error {
	if value == nil {
		return nil
	}
	if *value < 1 {
		return errors.New("页面生成并发数不能小于 1")
	}
	if *value > store.MaxSlideConcurrencyLimit {
		return errors.New("页面生成并发数不能超过 10")
	}
	return nil
}
