package httpapi

import (
	"context"

	"wty5.cn/ppt-gen/internal/store"
)

// unconfiguredStore is returned when storage has not been configured yet.
type unconfiguredStore struct{}

func (unconfiguredStore) Close() error                             { return nil }
func (unconfiguredStore) NeedsSetup(context.Context) (bool, error) { return true, nil }
func (unconfiguredStore) CreateUser(context.Context, store.CreateUserInput) (store.User, error) {
	return store.User{}, store.ErrInvalidStore
}
func (unconfiguredStore) GetUserByEmail(context.Context, string) (store.User, error) {
	return store.User{}, store.ErrInvalidStore
}
func (unconfiguredStore) GetUserByID(context.Context, string) (store.User, error) {
	return store.User{}, store.ErrInvalidStore
}
func (unconfiguredStore) ListUsers(context.Context) ([]store.User, error) {
	return nil, store.ErrInvalidStore
}
func (unconfiguredStore) UpdateUser(context.Context, string, store.UpdateUserInput) (store.User, error) {
	return store.User{}, store.ErrInvalidStore
}
func (unconfiguredStore) DeleteUser(context.Context, string) error { return store.ErrInvalidStore }
func (unconfiguredStore) CountAdmins(context.Context) (int, error) { return 0, store.ErrInvalidStore }
func (unconfiguredStore) CreateSession(context.Context, store.Session) error {
	return store.ErrInvalidStore
}
func (unconfiguredStore) GetSession(context.Context, string) (store.Session, error) {
	return store.Session{}, store.ErrInvalidStore
}
func (unconfiguredStore) DeleteSession(context.Context, string) error { return store.ErrInvalidStore }
func (unconfiguredStore) GetPromptSettings(context.Context) (store.PromptSettings, error) {
	return store.PromptSettings{}, store.ErrInvalidStore
}
func (unconfiguredStore) SavePromptSettings(context.Context, store.PromptSettings) error {
	return store.ErrInvalidStore
}
func (unconfiguredStore) GetSystemSettings(context.Context) (store.SystemSettings, error) {
	return store.SystemSettings{}, store.ErrInvalidStore
}
func (unconfiguredStore) SaveSystemSettings(context.Context, store.SystemSettings) error {
	return store.ErrInvalidStore
}
func (unconfiguredStore) CreateUserGroup(context.Context, store.CreateUserGroupInput) (store.UserGroup, error) {
	return store.UserGroup{}, store.ErrInvalidStore
}
func (unconfiguredStore) ListUserGroups(context.Context) ([]store.UserGroup, error) {
	return nil, store.ErrInvalidStore
}
func (unconfiguredStore) GetUserGroup(context.Context, string) (store.UserGroup, error) {
	return store.UserGroup{}, store.ErrInvalidStore
}
func (unconfiguredStore) UpdateUserGroup(context.Context, string, store.UpdateUserGroupInput) (store.UserGroup, error) {
	return store.UserGroup{}, store.ErrInvalidStore
}
func (unconfiguredStore) DeleteUserGroup(context.Context, string) error { return store.ErrInvalidStore }
func (unconfiguredStore) GetEffectiveQuota(context.Context, string, string) (store.EffectiveQuota, error) {
	return store.EffectiveQuota{}, store.ErrInvalidStore
}
func (unconfiguredStore) ReserveDailyQuota(context.Context, store.ReserveQuotaInput) (store.QuotaReservation, error) {
	return store.QuotaReservation{}, store.ErrInvalidStore
}
func (unconfiguredStore) CommitDailyQuota(context.Context, store.QuotaReservation, int) (store.EffectiveQuota, error) {
	return store.EffectiveQuota{}, store.ErrInvalidStore
}
func (unconfiguredStore) ReleaseDailyQuota(context.Context, store.QuotaReservation) error {
	return store.ErrInvalidStore
}
func (unconfiguredStore) ListDailyUsages(context.Context) ([]store.DailyUsage, error) {
	return nil, store.ErrInvalidStore
}
func (unconfiguredStore) UpsertDailyUsage(context.Context, store.DailyUsage) error {
	return store.ErrInvalidStore
}
func (unconfiguredStore) ListLLMProviders(context.Context) ([]store.LLMProvider, error) {
	return nil, store.ErrInvalidStore
}
func (unconfiguredStore) GetLLMProvider(context.Context, string) (store.LLMProvider, error) {
	return store.LLMProvider{}, store.ErrInvalidStore
}
func (unconfiguredStore) CreateLLMProvider(context.Context, store.CreateLLMProviderInput) (store.LLMProvider, error) {
	return store.LLMProvider{}, store.ErrInvalidStore
}
func (unconfiguredStore) UpdateLLMProvider(context.Context, string, store.UpdateLLMProviderInput) (store.LLMProvider, error) {
	return store.LLMProvider{}, store.ErrInvalidStore
}
func (unconfiguredStore) DeleteLLMProvider(context.Context, string) error {
	return store.ErrInvalidStore
}
