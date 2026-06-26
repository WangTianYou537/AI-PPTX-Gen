package store

import (
	"context"
	"errors"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidStore  = errors.New("invalid store")
	ErrQuotaExceeded = errors.New("quota exceeded")
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
	GetSystemSettings(ctx context.Context) (SystemSettings, error)
	SaveSystemSettings(ctx context.Context, settings SystemSettings) error
	CreateUserGroup(ctx context.Context, input CreateUserGroupInput) (UserGroup, error)
	ListUserGroups(ctx context.Context) ([]UserGroup, error)
	GetUserGroup(ctx context.Context, id string) (UserGroup, error)
	UpdateUserGroup(ctx context.Context, id string, input UpdateUserGroupInput) (UserGroup, error)
	DeleteUserGroup(ctx context.Context, id string) error
	GetEffectiveQuota(ctx context.Context, userID string, date string) (EffectiveQuota, error)
	ReserveDailyQuota(ctx context.Context, input ReserveQuotaInput) (QuotaReservation, error)
	CommitDailyQuota(ctx context.Context, reservation QuotaReservation, actualSlides int) (EffectiveQuota, error)
	ReleaseDailyQuota(ctx context.Context, reservation QuotaReservation) error
	ListDailyUsages(ctx context.Context) ([]DailyUsage, error)
	UpsertDailyUsage(ctx context.Context, usage DailyUsage) error
}
