package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	StorageJSON     = "json"
	StorageSQLite   = "sqlite"
	StoragePostgres = "postgres"
	StorageMySQL    = "mysql"
	StorageRedis    = "redis"
)

type Config struct {
	Kind      string    `json:"kind"`
	Path      string    `json:"path,omitempty"`
	DSN       string    `json:"dsn,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type StorageOption struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	DefaultPath string `json:"defaultPath,omitempty"`
	Description string `json:"description"`
}

func DefaultConfigPath() string { return "config.yml" }

func SupportedStorageOptions() []StorageOption {
	return []StorageOption{
		{Kind: StorageJSON, Label: "JSON 文件", Status: "supported", DefaultPath: "data/app.json", Description: "简单、无需数据库服务，适合本地或单机部署。"},
		{Kind: StorageSQLite, Label: "SQLite", Status: "supported", DefaultPath: "data/app.db", Description: "单文件数据库，推荐小型部署。"},
		{Kind: StoragePostgres, Label: "PostgreSQL", Status: "advanced", Description: "适合长期和多人部署，需要提供可连接的 PostgreSQL DSN。"},
		{Kind: StorageMySQL, Label: "MySQL", Status: "comingSoon", Description: "当前版本暂不支持，后续会补齐 MySQL 方言和驱动。"},
		{Kind: StorageRedis, Label: "Redis", Status: "notPrimaryStore", Description: "Redis 不适合作为主数据存储，未来可作为会话或缓存组件。"},
	}
}

func LoadConfig(path string) (Config, bool, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	cfg, err := parseConfig(content)
	if err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

func SaveConfig(path string, cfg Config) error {
	if path == "" {
		path = DefaultConfigPath()
	}
	cfg.UpdatedAt = time.Now().UTC()
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	payload := []byte(formatConfigYAML(cfg))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func parseConfig(content []byte) (Config, error) {
	return parseConfigYAML(strings.TrimSpace(string(content))), nil
}

func parseConfigYAML(content string) Config {
	var cfg Config
	inStorage := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "storage:" {
			inStorage = true
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = unquoteYAMLValue(strings.TrimSpace(value))
		switch {
		case inStorage && key == "kind", key == "storage.kind":
			cfg.Kind = value
		case inStorage && key == "path", key == "storage.path":
			cfg.Path = value
		case inStorage && key == "dsn", key == "storage.dsn":
			cfg.DSN = value
		case key == "updatedAt":
			if ts, err := time.Parse(time.RFC3339, value); err == nil {
				cfg.UpdatedAt = ts
			}
		}
	}
	return NormalizeConfig(cfg)
}

func unquoteYAMLValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "''" || value == `""` {
		return ""
	}
	if len(value) >= 2 {
		if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
			return strings.ReplaceAll(value[1:len(value)-1], `\"`, `"`)
		}
	}
	return value
}

func formatConfigYAML(cfg Config) string {
	return fmt.Sprintf("storage:\n  kind: %s\n  path: %s\n  dsn: %s\nupdatedAt: %s\n",
		quoteYAMLValue(cfg.Kind),
		quoteYAMLValue(cfg.Path),
		quoteYAMLValue(cfg.DSN),
		quoteYAMLValue(cfg.UpdatedAt.Format(time.RFC3339)),
	)
}

func quoteYAMLValue(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func ValidateConfig(cfg Config) error {
	cfg.Kind = strings.TrimSpace(strings.ToLower(cfg.Kind))
	switch cfg.Kind {
	case StorageJSON, StorageSQLite:
		if strings.TrimSpace(cfg.Path) == "" && strings.TrimSpace(cfg.DSN) == "" {
			return fmt.Errorf("%w: 请填写数据文件路径", ErrInvalidStore)
		}
	case StoragePostgres:
		if strings.TrimSpace(cfg.DSN) == "" {
			return fmt.Errorf("%w: 请填写 PostgreSQL DSN", ErrInvalidStore)
		}
	case StorageMySQL:
		return fmt.Errorf("%w: 当前版本暂不支持 MySQL", ErrInvalidStore)
	case StorageRedis:
		return fmt.Errorf("%w: Redis 当前不支持作为主数据存储", ErrInvalidStore)
	default:
		return fmt.Errorf("%w: 存储类型不正确", ErrInvalidStore)
	}
	return nil
}

func NormalizeConfig(cfg Config) Config {
	cfg.Kind = strings.TrimSpace(strings.ToLower(cfg.Kind))
	cfg.Path = strings.TrimSpace(cfg.Path)
	cfg.DSN = strings.TrimSpace(cfg.DSN)
	if cfg.Kind == StorageJSON && cfg.Path == "" && cfg.DSN == "" {
		cfg.Path = "data/app.json"
	}
	if cfg.Kind == StorageSQLite && cfg.Path == "" && cfg.DSN == "" {
		cfg.Path = "data/app.db"
	}
	return cfg
}

func OpenConfiguredStore(ctx context.Context, cfg Config) (Store, error) {
	cfg = NormalizeConfig(cfg)
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	switch cfg.Kind {
	case StorageJSON:
		path := cfg.Path
		if path == "" {
			path = cfg.DSN
		}
		return NewJSONStore(path)
	case StorageSQLite:
		dsn := cfg.Path
		if dsn == "" {
			dsn = cfg.DSN
		}
		return NewSQLStoreContext(ctx, "sqlite", dsn, "sqlite")
	case StoragePostgres:
		return NewSQLStoreContext(ctx, "postgres", cfg.DSN, "postgres")
	default:
		return nil, ErrInvalidStore
	}
}

func RedactConfig(cfg Config) Config {
	cfg.DSN = redactDSN(cfg.DSN)
	return cfg
}

func redactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	parsed, err := url.Parse(dsn)
	if err == nil && parsed.User != nil {
		username := parsed.User.Username()
		if _, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(username, "******")
		}
		return parsed.String()
	}
	return dsn
}
