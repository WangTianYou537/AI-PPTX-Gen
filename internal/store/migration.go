package store

import "context"

type Snapshot struct {
	Users          []User         `json:"users"`
	PromptSettings PromptSettings `json:"promptSettings"`
	SystemSettings SystemSettings `json:"systemSettings"`
	UserGroups     []UserGroup    `json:"userGroups"`
	DailyUsages    []DailyUsage   `json:"dailyUsages"`
	LLMProviders   []LLMProvider  `json:"llmProviders"`
}

func ExportSnapshot(ctx context.Context, source Store) (Snapshot, error) {
	users, err := source.ListUsers(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	settings, err := source.GetPromptSettings(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	systemSettings, err := source.GetSystemSettings(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	groups, err := source.ListUserGroups(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	usages, err := source.ListDailyUsages(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	providers, err := source.ListLLMProviders(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Users: users, PromptSettings: settings, SystemSettings: systemSettings, UserGroups: groups, DailyUsages: usages, LLMProviders: providers}, nil
}

func ImportSnapshot(ctx context.Context, target Store, snapshot Snapshot) error {
	for _, group := range snapshot.UserGroups {
		_, err := target.CreateUserGroup(ctx, CreateUserGroupInput{ID: group.ID, Name: group.Name, Description: group.Description, DailyPPTLimit: group.DailyPPTLimit, DailySlideLimit: group.DailySlideLimit, SlideConcurrencyLimit: group.SlideConcurrencyLimit, IsDefault: group.IsDefault, CreatedAt: &group.CreatedAt, UpdatedAt: &group.UpdatedAt})
		if err != nil && err != ErrAlreadyExists {
			// The built-in default group already exists in fresh stores.
			if group.ID != DefaultUserGroupID {
				return err
			}
		}
		if group.ID == DefaultUserGroupID {
			_, _ = target.UpdateUserGroup(ctx, DefaultUserGroupID, UpdateUserGroupInput{Name: &group.Name, Description: &group.Description, DailyPPTLimit: &group.DailyPPTLimit, DailySlideLimit: &group.DailySlideLimit, SlideConcurrencyLimit: &group.SlideConcurrencyLimit, IsDefault: &group.IsDefault})
		}
	}
	if snapshot.SystemSettings.DefaultUserGroupID != "" {
		if err := target.SaveSystemSettings(ctx, snapshot.SystemSettings); err != nil {
			return err
		}
	}
	for _, user := range snapshot.Users {
		_, err := target.CreateUser(ctx, CreateUserInput{ID: user.ID, Email: user.Email, Username: user.Username, PasswordHash: user.PasswordHash, Role: user.Role, Disabled: user.Disabled, GroupID: user.GroupID, DailyPPTLimit: user.DailyPPTLimit, DailySlideLimit: user.DailySlideLimit, SlideConcurrencyLimit: user.SlideConcurrencyLimit, CreatedAt: &user.CreatedAt, UpdatedAt: &user.UpdatedAt})
		if err != nil && err != ErrAlreadyExists {
			return err
		}
	}
	for _, provider := range snapshot.LLMProviders {
		_, err := target.CreateLLMProvider(ctx, CreateLLMProviderInput{ID: provider.ID, Name: provider.Name, Kind: provider.Kind, BaseURL: provider.BaseURL, APIKey: provider.APIKey, Enabled: provider.Enabled})
		if err != nil && err != ErrAlreadyExists {
			return err
		}
	}
	if err := target.SavePromptSettings(ctx, snapshot.PromptSettings); err != nil {
		return err
	}
	for _, usage := range snapshot.DailyUsages {
		if err := target.UpsertDailyUsage(ctx, usage); err != nil {
			return err
		}
	}
	return nil
}
