package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port            string
	ProxyAPIKey     string
	BaseURL         string
	Accounts        []string // DeepSeek web tokens, one per account
	AccountsFile    string
	DefaultModel    string
	ConversationTTL time.Duration
	MaxConvs        int
}

// Load reads configuration from the environment. Accounts come from
// DEEPSEEK_TOKEN plus one-per-line entries in DEEPSEEK_ACCOUNTS_FILE.
func Load() (*Config, error) {
	cfg := &Config{
		Port:            env("PORT", "8080"),
		ProxyAPIKey:     strings.TrimSpace(os.Getenv("PROXY_API_KEY")),
		BaseURL:         strings.TrimRight(env("DEEPSEEK_BASE_URL", "https://chat.deepseek.com"), "/"),
		AccountsFile:    env("DEEPSEEK_ACCOUNTS_FILE", "accounts.txt"),
		DefaultModel:    env("DEFAULT_MODEL", "deepseek-chat"),
		ConversationTTL: 30 * time.Minute,
		MaxConvs:        1024,
	}
	if cfg.ProxyAPIKey == "" {
		return nil, fmt.Errorf("PROXY_API_KEY is required")
	}
	if v := os.Getenv("CONVERSATION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CONVERSATION_TTL: %w", err)
		}
		cfg.ConversationTTL = d
	}
	if v := os.Getenv("MAX_CONVERSATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid MAX_CONVERSATIONS: %q", v)
		}
		cfg.MaxConvs = n
	}
	if t := strings.TrimSpace(os.Getenv("DEEPSEEK_TOKEN")); t != "" {
		cfg.Accounts = append(cfg.Accounts, t)
	}
	fileAccounts, err := loadAccountsFile(cfg.AccountsFile)
	if err != nil {
		return nil, err
	}
	cfg.Accounts = append(cfg.Accounts, fileAccounts...)
	if len(cfg.Accounts) == 0 {
		return nil, fmt.Errorf("no DeepSeek account configured; set DEEPSEEK_TOKEN or %s", cfg.AccountsFile)
	}
	return cfg, nil
}

func loadAccountsFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
