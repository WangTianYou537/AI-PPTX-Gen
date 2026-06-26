package store

import (
	"strings"
	"time"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	DefaultUserGroupID = "default"
	QuotaSourceUser    = "user"
	QuotaSourceGroup   = "group"
)

type User struct {
	ID              string    `json:"id"`
	Email           string    `json:"email"`
	Username        string    `json:"username"`
	PasswordHash    string    `json:"-"`
	Role            string    `json:"role"`
	Disabled        bool      `json:"disabled"`
	GroupID         string    `json:"groupId"`
	DailyPPTLimit   *int      `json:"dailyPPTLimit,omitempty"`
	DailySlideLimit *int      `json:"dailySlideLimit,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type storedUser struct {
	ID              string    `json:"id"`
	Email           string    `json:"email"`
	Username        string    `json:"username"`
	PasswordHash    string    `json:"passwordHash"`
	Role            string    `json:"role"`
	Disabled        bool      `json:"disabled"`
	GroupID         string    `json:"groupId"`
	DailyPPTLimit   *int      `json:"dailyPPTLimit,omitempty"`
	DailySlideLimit *int      `json:"dailySlideLimit,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	TokenHash string    `json:"tokenHash"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type UserGroup struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	DailyPPTLimit   int       `json:"dailyPPTLimit"`
	DailySlideLimit int       `json:"dailySlideLimit"`
	IsDefault       bool      `json:"isDefault"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type SystemSettings struct {
	RegistrationEnabled bool      `json:"registrationEnabled"`
	DefaultUserGroupID  string    `json:"defaultUserGroupId"`
	UpdatedAt           time.Time `json:"updatedAt"`
	UpdatedBy           string    `json:"updatedBy"`
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
	ID              string
	Name            string
	Description     string
	DailyPPTLimit   int
	DailySlideLimit int
	IsDefault       bool
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

type UpdateUserGroupInput struct {
	Name            *string
	Description     *string
	DailyPPTLimit   *int
	DailySlideLimit *int
	IsDefault       *bool
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

type ModelConfig struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
	BaseURL  string `json:"baseURL"`
	Model    string `json:"model"`
}

type GenerationRoleSettings struct {
	SystemPrompt  string      `json:"systemPrompt"`
	SupportsTools bool        `json:"supportsTools"`
	ModelConfig   ModelConfig `json:"modelConfig"`
}

type PromptSettings struct {
	Architect GenerationRoleSettings `json:"architect"`
	SVG       GenerationRoleSettings `json:"svg"`
	// Deprecated fields are kept so older JSON stores can be read and migrated lazily.
	ArchitectSystemPrompt string    `json:"architectSystemPrompt,omitempty"`
	SVGSystemPrompt       string    `json:"svgSystemPrompt,omitempty"`
	UpdatedAt             time.Time `json:"updatedAt"`
	UpdatedBy             string    `json:"updatedBy"`
}

type CreateUserInput struct {
	ID              string
	Email           string
	Username        string
	PasswordHash    string
	Role            string
	Disabled        bool
	GroupID         string
	DailyPPTLimit   *int
	DailySlideLimit *int
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
}

type UpdateUserInput struct {
	Email                *string
	Username             *string
	PasswordHash         *string
	Role                 *string
	Disabled             *bool
	GroupID              *string
	DailyPPTLimit        *int
	DailySlideLimit      *int
	ClearDailyPPTLimit   bool
	ClearDailySlideLimit bool
}

func DefaultUserGroup(now time.Time) UserGroup {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return UserGroup{ID: DefaultUserGroupID, Name: "默认用户组", Description: "系统默认用户组", DailyPPTLimit: 0, DailySlideLimit: 100, IsDefault: true, CreatedAt: now, UpdatedAt: now}
}

func DefaultSystemSettings(now time.Time) SystemSettings {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return SystemSettings{RegistrationEnabled: true, DefaultUserGroupID: DefaultUserGroupID, UpdatedAt: now}
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
	return User{ID: user.ID, Email: user.Email, Username: user.Username, PasswordHash: user.PasswordHash, Role: user.Role, Disabled: user.Disabled, GroupID: user.GroupID, DailyPPTLimit: cloneIntPtr(user.DailyPPTLimit), DailySlideLimit: cloneIntPtr(user.DailySlideLimit), CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}
}

func storedFromUser(user User) storedUser {
	return storedUser{ID: user.ID, Email: user.Email, Username: user.Username, PasswordHash: user.PasswordHash, Role: user.Role, Disabled: user.Disabled, GroupID: user.GroupID, DailyPPTLimit: cloneIntPtr(user.DailyPPTLimit), DailySlideLimit: cloneIntPtr(user.DailySlideLimit), CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
