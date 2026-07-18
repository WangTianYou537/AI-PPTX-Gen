package store

type sqlMigration struct {
	Version              int
	AllowDuplicateColumn bool
	Statements           []string
}

// sqlMigrations is the ordered schema evolution for SQL stores.
// Version 1 creates the current baseline schema (including historically
// added columns) so fresh databases do not depend on ALTER compatibility.
// Version 2 only runs legacy ALTER statements for older databases that
// already had schema_version < 2 / partial older tables.
var sqlMigrations = []sqlMigration{
	{
		Version: 1,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS users (
				id TEXT PRIMARY KEY,
				email TEXT NOT NULL UNIQUE,
				username TEXT NOT NULL DEFAULT '',
				password_hash TEXT NOT NULL,
				role TEXT NOT NULL,
				disabled BOOLEAN NOT NULL DEFAULT FALSE,
				group_id TEXT NOT NULL DEFAULT 'default',
				daily_ppt_limit INTEGER NULL,
				daily_slide_limit INTEGER NULL,
				slide_concurrency_limit INTEGER NULL,
				created_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS sessions (
				id TEXT PRIMARY KEY,
				user_id TEXT NOT NULL,
				token_hash TEXT NOT NULL UNIQUE,
				expires_at TIMESTAMP NOT NULL,
				created_at TIMESTAMP NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS prompt_settings (
				id INTEGER PRIMARY KEY,
				architect_system_prompt TEXT NOT NULL DEFAULT '',
				svg_system_prompt TEXT NOT NULL DEFAULT '',
				architect_model_provider TEXT NOT NULL DEFAULT '',
				architect_model_api_key TEXT NOT NULL DEFAULT '',
				architect_model_base_url TEXT NOT NULL DEFAULT '',
				architect_model_model TEXT NOT NULL DEFAULT '',
				architect_request_json TEXT NOT NULL DEFAULT '',
				svg_model_provider TEXT NOT NULL DEFAULT '',
				svg_model_api_key TEXT NOT NULL DEFAULT '',
				svg_model_base_url TEXT NOT NULL DEFAULT '',
				svg_model_model TEXT NOT NULL DEFAULT '',
				svg_request_json TEXT NOT NULL DEFAULT '',
				updated_at TIMESTAMP NOT NULL,
				updated_by TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE TABLE IF NOT EXISTS system_settings (
				id INTEGER PRIMARY KEY,
				registration_enabled BOOLEAN NOT NULL DEFAULT TRUE,
				default_user_group_id TEXT NOT NULL DEFAULT 'default',
				default_slide_concurrency_limit INTEGER NOT NULL DEFAULT 2,
				updated_at TIMESTAMP NOT NULL,
				updated_by TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE TABLE IF NOT EXISTS user_groups (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				daily_ppt_limit INTEGER NOT NULL,
				daily_slide_limit INTEGER NOT NULL,
				slide_concurrency_limit INTEGER NOT NULL DEFAULT 2,
				is_default BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS daily_usage (
				user_id TEXT NOT NULL,
				usage_date TEXT NOT NULL,
				ppt_used INTEGER NOT NULL DEFAULT 0,
				slides_used INTEGER NOT NULL DEFAULT 0,
				ppt_reserved INTEGER NOT NULL DEFAULT 0,
				slides_reserved INTEGER NOT NULL DEFAULT 0,
				updated_at TIMESTAMP NOT NULL,
				PRIMARY KEY (user_id, usage_date)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash)`,
			`CREATE INDEX IF NOT EXISTS idx_users_group_id ON users(group_id)`,
			`CREATE INDEX IF NOT EXISTS idx_daily_usage_date ON daily_usage(usage_date)`,
		},
	},
	{
		Version:              2,
		AllowDuplicateColumn: true,
		Statements: []string{
			"ALTER TABLE users ADD COLUMN username TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE users ADD COLUMN group_id TEXT NOT NULL DEFAULT 'default'",
			"ALTER TABLE users ADD COLUMN daily_ppt_limit INTEGER NULL",
			"ALTER TABLE users ADD COLUMN daily_slide_limit INTEGER NULL",
			"ALTER TABLE users ADD COLUMN slide_concurrency_limit INTEGER NULL",
			"ALTER TABLE user_groups ADD COLUMN slide_concurrency_limit INTEGER NOT NULL DEFAULT 2",
			"ALTER TABLE system_settings ADD COLUMN default_slide_concurrency_limit INTEGER NOT NULL DEFAULT 2",
			"ALTER TABLE prompt_settings ADD COLUMN architect_request_json TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE prompt_settings ADD COLUMN svg_request_json TEXT NOT NULL DEFAULT ''",
		},
	},
	{
		Version:              3,
		AllowDuplicateColumn: true,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS llm_providers (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				kind TEXT NOT NULL,
				base_url TEXT NOT NULL DEFAULT '',
				api_key TEXT NOT NULL DEFAULT '',
				enabled BOOLEAN NOT NULL DEFAULT TRUE,
				created_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL
			)`,
			"ALTER TABLE prompt_settings ADD COLUMN architect_provider_id TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE prompt_settings ADD COLUMN architect_model TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE prompt_settings ADD COLUMN svg_provider_id TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE prompt_settings ADD COLUMN svg_model TEXT NOT NULL DEFAULT ''",
		},
	},
}
