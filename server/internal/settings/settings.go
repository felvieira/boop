// Package settings is a small key/value store backed by the settings table.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/chrisgreg/boop/server/internal/ids"
	"github.com/chrisgreg/boop/server/internal/secretbox"
)

// Well-known keys.
const (
	KeyRetentionDays             = "retention_days"
	KeySetupCompleted            = "setup_completed"
	KeyRedactKeys                = "redact_keys" // comma-separated, added to the built-in list
	KeyMCPEnabled                = "mcp_enabled" // "false" turns the /mcp endpoint off; unset = on
	KeyVAPIDPublicKey            = "vapid_public_key"
	KeyVAPIDPrivateKey           = "vapid_private_key"
	KeyVAPIDSubject              = "vapid_subject"
	KeyOpenRouterAPIKey          = "openrouter_api_key"
	KeyGitHubToken               = "github_api_token"
	KeySupportGitHubRepositories = "support_github_repositories"
	KeyOpenRouterModel           = "openrouter_model"
	KeySupportTriageInstructions = "support_triage_instructions"
	KeySupportDigestEnabled      = "support_digest_enabled"
	KeySupportDigestHour         = "support_digest_hour"
	KeySupportDigestLastDate     = "support_digest_last_date"
	KeySupportDigestLastSentAt   = "support_digest_last_sent_at"
)

// Store reads and writes settings.
type Store struct {
	db  *sql.DB
	box *secretbox.Box
}

// New returns a Store.
func New(db *sql.DB, boxes ...*secretbox.Box) *Store {
	var box *secretbox.Box
	if len(boxes) > 0 {
		box = boxes[0]
	}
	return &Store{db: db, box: box}
}

// Get returns the value for key, or def when unset.
func (s *Store) Get(ctx context.Context, key, def string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	return v, err
}

// Set writes key.
func (s *Store) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, ids.Now())
	return err
}

// SetSecret encrypts a secret before writing it to SQLite.
func (s *Store) SetSecret(ctx context.Context, key, value string) error {
	if s.box == nil {
		return errors.New("secret encryption is unavailable")
	}
	sealed, err := s.box.Encrypt([]byte(value), []byte("boop.settings:"+key+":v1"))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, "enc:v1:"+sealed, ids.Now())
	return err
}

// GetSecret decrypts a secret setting. Missing secrets return an empty value.
func (s *Store) GetSecret(ctx context.Context, key string) (string, error) {
	stored, err := s.Get(ctx, key, "")
	if err != nil || stored == "" {
		return "", err
	}
	if !strings.HasPrefix(stored, "enc:v1:") {
		return "", errors.New("secret setting is not encrypted")
	}
	if s.box == nil {
		return "", errors.New("secret encryption is unavailable")
	}
	plaintext, err := s.box.Decrypt(strings.TrimPrefix(stored, "enc:v1:"), []byte("boop.settings:"+key+":v1"))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// HasSecret checks whether a secret setting exists without reading its contents.
func (s *Store) HasSecret(ctx context.Context, key string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM settings WHERE key = ?)`, key).Scan(&exists)
	return exists == 1, err
}

// DeleteSecret removes a secret setting without decrypting it.
func (s *Store) DeleteSecret(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}

// OpenRouterConfig resolves the UI-saved global configuration before environment fallbacks.
func (s *Store) OpenRouterConfig(ctx context.Context, envAPIKey, envModel string) (OpenRouterConfig, error) {
	config := OpenRouterConfig{APIKey: strings.TrimSpace(envAPIKey), Model: strings.TrimSpace(envModel)}
	if config.Model == "" {
		config.Model = "google/gemini-3.8-flash"
	}
	if model, err := s.Get(ctx, KeyOpenRouterModel, ""); err != nil {
		return config, err
	} else if strings.TrimSpace(model) != "" {
		config.Model = strings.TrimSpace(model)
	}
	apiKey, err := s.GetSecret(ctx, KeyOpenRouterAPIKey)
	if err != nil {
		if exists, hasErr := s.HasSecret(ctx, KeyOpenRouterAPIKey); hasErr == nil && exists {
			config.APIKey = ""
			config.KeySource = "settings"
		}
		return config, err
	}
	if apiKey != "" {
		config.APIKey = apiKey
		config.KeySource = "settings"
	} else if config.APIKey != "" {
		config.KeySource = "environment"
	}
	return config, nil
}

// OpenRouterConfig is the effective global triage provider configuration.
type OpenRouterConfig struct {
	APIKey    string
	Model     string
	KeySource string
}

// GetInt returns key parsed as an int, or def when unset or unparsable.
func (s *Store) GetInt(ctx context.Context, key string, def int) (int, error) {
	v, err := s.Get(ctx, key, "")
	if err != nil || v == "" {
		return def, err
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, nil
	}
	return n, nil
}

// GetBool returns key as a bool ("true"/"1").
func (s *Store) GetBool(ctx context.Context, key string) (bool, error) {
	v, err := s.Get(ctx, key, "")
	return v == "true" || v == "1", err
}

// GetList returns a comma-separated setting as a trimmed, non-empty list.
func (s *Store) GetList(ctx context.Context, key string) ([]string, error) {
	v, err := s.Get(ctx, key, "")
	if err != nil || v == "" {
		return nil, err
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// SetDefault writes key only when it is not already set.
func (s *Store) SetDefault(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, key, value, ids.Now())
	return err
}
