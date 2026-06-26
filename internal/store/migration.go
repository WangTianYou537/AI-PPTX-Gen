package store

import "context"

type Snapshot struct {
	Users          []User         `json:"users"`
	PromptSettings PromptSettings `json:"promptSettings"`
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
	return Snapshot{Users: users, PromptSettings: settings}, nil
}

func ImportSnapshot(ctx context.Context, target Store, snapshot Snapshot) error {
	for _, user := range snapshot.Users {
		_, err := target.CreateUser(ctx, CreateUserInput{
			Email:        user.Email,
			PasswordHash: user.PasswordHash,
			Role:         user.Role,
			Disabled:     user.Disabled,
		})
		if err != nil && err != ErrAlreadyExists {
			return err
		}
	}
	return target.SavePromptSettings(ctx, snapshot.PromptSettings)
}
