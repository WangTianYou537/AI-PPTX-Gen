package store

import "time"

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type storedUser struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"passwordHash"`
	Role         string    `json:"role"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	TokenHash string    `json:"tokenHash"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
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
	Email        string
	PasswordHash string
	Role         string
	Disabled     bool
}

type UpdateUserInput struct {
	Email        *string
	PasswordHash *string
	Role         *string
	Disabled     *bool
}

func publicUser(user storedUser) User {
	return User{
		ID:           user.ID,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
		Disabled:     user.Disabled,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
	}
}
