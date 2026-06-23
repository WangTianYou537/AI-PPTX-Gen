package store

import (
	"context"
	"errors"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidStore  = errors.New("invalid store")
)

type Store interface {
	Close() error
	NeedsSetup(ctx context.Context) (bool, error)
	CreateUser(ctx context.Context, input CreateUserInput) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUser(ctx context.Context, id string, input UpdateUserInput) (User, error)
	DeleteUser(ctx context.Context, id string) error
	CountAdmins(ctx context.Context) (int, error)
	CreateSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, tokenHash string) (Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	GetPromptSettings(ctx context.Context) (PromptSettings, error)
	SavePromptSettings(ctx context.Context, settings PromptSettings) error
}
