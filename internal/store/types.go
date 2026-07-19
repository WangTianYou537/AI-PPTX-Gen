package store

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	DefaultUserGroupID = "default"
	QuotaSourceUser    = "user"
	QuotaSourceGroup   = "group"

	DefaultSlideConcurrencyLimit = 2
	MaxSlideConcurrencyLimit     = 10
)

type User struct {
	ID                    string    `json:"id"`
	Email                 string    `json:"email"`
	Username              string    `json:"username"`
	PasswordHash          string    `json:"-"`
	Role                  string    `json:"role"`
	Disabled              bool      `json:"disabled"`
	GroupID               string    `json:"groupId"`
	DailyPPTLimit         *int      `json:"dailyPPTLimit,omitempty"`
	DailySlideLimit       *int      `json:"dailySlideLimit,omitempty"`
	SlideConcurrencyLimit *int      `json:"slideConcurrencyLimit,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type storedUser struct {
	ID                    string    `json:"id"`
	Email                 string    `json:"email"`
	Username              string    `json:"username"`
	PasswordHash          string    `json:"passwordHash"`
	Role                  string    `json:"role"`
	Disabled              bool      `json:"disabled"`
	GroupID               string    `json:"groupId"`
	DailyPPTLimit         *int      `json:"dailyPPTLimit,omitempty"`
	DailySlideLimit       *int      `json:"dailySlideLimit,omitempty"`
	SlideConcurrencyLimit *int      `json:"slideConcurrencyLimit,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	TokenHash string    `json:"tokenHash"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type UserGroup struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	DailyPPTLimit         int       `json:"dailyPPTLimit"`
	DailySlideLimit       int       `json:"dailySlideLimit"`
	SlideConcurrencyLimit int       `json:"slideConcurrencyLimit"`
	IsDefault             bool      `json:"isDefault"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type SystemSettings struct {
	RegistrationEnabled          bool      `json:"registrationEnabled"`
	DefaultUserGroupID           string    `json:"defaultUserGroupId"`
	DefaultSlideConcurrencyLimit int       `json:"defaultSlideConcurrencyLimit"`
	UpdatedAt                    time.Time `json:"updatedAt"`
	UpdatedBy                    string    `json:"updatedBy"`
}

type DailyUsage struct {
	UserID         string    `json:"userId"`
	Date           string    `json:"date"`
	PPTUsed        int       `json:"pptUsed"`
	SlidesUsed     int       `json:"slidesUsed"`
	PPTReserved    int       `json:"pptReserved"`
	SlidesReserved int       `json:"slidesReserved"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type EffectiveQuota struct {
	Date            string `json:"date"`
	DailyPPTLimit   int    `json:"dailyPPTLimit"`
	DailySlideLimit int    `json:"dailySlideLimit"`
	PPTUsed         int    `json:"pptUsed"`
	SlidesUsed      int    `json:"slidesUsed"`
	PPTReserved     int    `json:"pptReserved"`
	SlidesReserved  int    `json:"slidesReserved"`
	PPTRemaining    int    `json:"pptRemaining"`
	SlidesRemaining int    `json:"slidesRemaining"`
	Source          string `json:"source"`
	GroupID         string `json:"groupId"`
	GroupName       string `json:"groupName"`
}

type CreateUserGroupInput struct {
	ID                    string
	Name                  string
	Description           string
	DailyPPTLimit         int
	DailySlideLimit       int
	SlideConcurrencyLimit int
	IsDefault             bool
	CreatedAt             *time.Time
	UpdatedAt             *time.Time
}

type UpdateUserGroupInput struct {
	Name                  *string
	Description           *string
	DailyPPTLimit         *int
	DailySlideLimit       *int
	SlideConcurrencyLimit *int
	IsDefault             *bool
}

type ReserveQuotaInput struct {
	UserID string
	Date   string
	PPTs   int
	Slides int
}

type QuotaReservation struct {
	UserID string `json:"userId"`
	Date   string `json:"date"`
	PPTs   int    `json:"ppts"`
	Slides int    `json:"slides"`
}

type LLMProvider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // openai | openai-responses | gemini | claude
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
	// Proxy is an optional HTTP/HTTPS/SOCKS proxy URL for outbound LLM requests.
	// Example: http://127.0.0.1:7890  or socks5://127.0.0.1:1080
	Proxy     string    `json:"proxy,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CreateLLMProviderInput struct {
	ID      string
	Name    string
	Kind    string
	BaseURL string
	APIKey  string
	Proxy   string
	Enabled bool
}

type UpdateLLMProviderInput struct {
	Name    *string
	Kind    *string
	BaseURL *string
	APIKey  *string
	Proxy   *string
	Enabled *bool
}

type Upload struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	Path        string    `json:"path"`
	CreatedAt   time.Time `json:"createdAt"`
}

type GenerationJobRecord struct {
	ID       string `json:"id"`
	UserID   string `json:"userId"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Error    string `json:"error,omitempty"`
	// ParentJobID links retry/AI-edit tasks under an original generation job.
	// Empty means top-level job shown in the main task list.
	ParentJobID string `json:"parentJobId,omitempty"`
	// Label is a short human description, e.g. "重试 slide-3" / "AI修改 slide-1" / "批量重试失败页".
	Label       string          `json:"label,omitempty"`
	PayloadJSON json.RawMessage `json:"payloadJson,omitempty"`
	ResultJSON  json.RawMessage `json:"resultJson,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	StartedAt   *time.Time      `json:"startedAt,omitempty"`
	FinishedAt  *time.Time      `json:"finishedAt,omitempty"`
}

type ModelConfig struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
	BaseURL  string `json:"baseURL"`
	Model    string `json:"model"`
	Proxy    string `json:"proxy,omitempty"`
}

type GenerationRoleSettings struct {
	SystemPrompt  string `json:"systemPrompt"`
	SupportsTools bool   `json:"supportsTools"`
	// RequestJSON is an optional JSON object merged into the provider default request body.
	// Example: {"temperature":0.2,"max_tokens":8192,"top_p":0.9}
	RequestJSON string `json:"requestJson"`
	// ProviderID references a managed LLM provider. When set, credentials come from that provider.
	ProviderID string `json:"providerId,omitempty"`
	// Model is the selected model id when using ProviderID.
	Model string `json:"model,omitempty"`
	// ModelConfig is filled at runtime after resolving ProviderID + Model.
	ModelConfig ModelConfig `json:"modelConfig"`
}

type PromptSettings struct {
	Architect GenerationRoleSettings `json:"architect"`
	SVG       GenerationRoleSettings `json:"svg"`
	// Theme is the theme-color planner role: turns free-form visual style into a locked palette.
	Theme GenerationRoleSettings `json:"theme"`
	// ArchitectWorkflowJSON stores the configurable agent workflow for outline generation.
	// JSON shape: {version,name,steps:[{id,kind,name,enabled,condition,providerId,model,systemPrompt,requestJson}]}
	ArchitectWorkflowJSON string `json:"architectWorkflowJson,omitempty"`
	// Deprecated fields are kept so older JSON stores can be read and migrated lazily.
	ArchitectSystemPrompt string    `json:"architectSystemPrompt,omitempty"`
	SVGSystemPrompt       string    `json:"svgSystemPrompt,omitempty"`
	UpdatedAt             time.Time `json:"updatedAt"`
	UpdatedBy             string    `json:"updatedBy"`
}

type CreateUserInput struct {
	ID                    string
	Email                 string
	Username              string
	PasswordHash          string
	Role                  string
	Disabled              bool
	GroupID               string
	DailyPPTLimit         *int
	DailySlideLimit       *int
	SlideConcurrencyLimit *int
	CreatedAt             *time.Time
	UpdatedAt             *time.Time
}

type UpdateUserInput struct {
	Email                      *string
	Username                   *string
	PasswordHash               *string
	Role                       *string
	Disabled                   *bool
	GroupID                    *string
	DailyPPTLimit              *int
	DailySlideLimit            *int
	SlideConcurrencyLimit      *int
	ClearDailyPPTLimit         bool
	ClearDailySlideLimit       bool
	ClearSlideConcurrencyLimit bool
}

func DefaultUserGroup(now time.Time) UserGroup {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return UserGroup{ID: DefaultUserGroupID, Name: "默认用户组", Description: "系统默认用户组", DailyPPTLimit: 0, DailySlideLimit: 100, SlideConcurrencyLimit: DefaultSlideConcurrencyLimit, IsDefault: true, CreatedAt: now, UpdatedAt: now}
}

func DefaultSystemSettings(now time.Time) SystemSettings {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return SystemSettings{RegistrationEnabled: true, DefaultUserGroupID: DefaultUserGroupID, DefaultSlideConcurrencyLimit: DefaultSlideConcurrencyLimit, UpdatedAt: now}
}

func DefaultUsername(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "用户"
	}
	if at := strings.Index(email, "@"); at > 0 {
		return email[:at]
	}
	return email
}

func publicUser(user storedUser) User {
	return User{ID: user.ID, Email: user.Email, Username: user.Username, PasswordHash: user.PasswordHash, Role: user.Role, Disabled: user.Disabled, GroupID: user.GroupID, DailyPPTLimit: cloneIntPtr(user.DailyPPTLimit), DailySlideLimit: cloneIntPtr(user.DailySlideLimit), SlideConcurrencyLimit: cloneIntPtr(user.SlideConcurrencyLimit), CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}
}

func storedFromUser(user User) storedUser {
	return storedUser{ID: user.ID, Email: user.Email, Username: user.Username, PasswordHash: user.PasswordHash, Role: user.Role, Disabled: user.Disabled, GroupID: user.GroupID, DailyPPTLimit: cloneIntPtr(user.DailyPPTLimit), DailySlideLimit: cloneIntPtr(user.DailySlideLimit), SlideConcurrencyLimit: cloneIntPtr(user.SlideConcurrencyLimit), CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
