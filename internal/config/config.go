package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort                   = "8080"
	defaultCORSAllowedOrigins     = "http://localhost:5173"
	defaultReadTimeoutSeconds     = 10
	defaultWriteTimeoutSeconds    = 10
	defaultIdleTimeoutSeconds     = 60
	defaultShutdownTimeoutSeconds = 10
)

type Config struct {
	Environment              string
	Port                     string
	DatabaseURL              string
	PublicCORSAllowedOrigins []string
	ReadTimeout              time.Duration
	WriteTimeout             time.Duration
	IdleTimeout              time.Duration
	ShutdownTimeout          time.Duration
	EmailProvider            string
	EmailFrom                string
	EmailReplyTo             string
	EmailAllowedRecipients   []string
	ResendAPIKeyFile         string
	ReviewRecipient          string
	ReviewBaseURL            string
	HostedSignupBaseURL      string
	TokenSecretRoot          string
	ProvisioningSecret       []byte
	ReviewLifetime           time.Duration
	GrantLifetime            time.Duration
	ClaimLifetime            time.Duration
}

type DatabaseConfig struct {
	DBName   string
	Host     string
	Password string
	Port     int
	SSLMode  string
	User     string
}

func Load() (Config, error) {
	environment := strings.ToLower(strings.TrimSpace(envOrDefault("ENV", "development")))
	cfg := Config{
		Environment:              environment,
		Port:                     envOrDefault("PORT", defaultPort),
		DatabaseURL:              strings.TrimSpace(os.Getenv("DATABASE_URL")),
		PublicCORSAllowedOrigins: splitCSV(envOrDefault("PUBLIC_CORS_ALLOWED_ORIGINS", defaultCORSAllowedOrigins)),
		EmailProvider:            strings.ToLower(strings.TrimSpace(envOrDefault("MARKETING_EMAIL_PROVIDER", "disabled"))),
		EmailFrom:                strings.TrimSpace(os.Getenv("MARKETING_EMAIL_FROM")),
		EmailReplyTo:             strings.TrimSpace(os.Getenv("MARKETING_EMAIL_REPLY_TO")),
		EmailAllowedRecipients:   splitCSV(os.Getenv("MARKETING_EMAIL_ALLOWED_RECIPIENTS")),
		ResendAPIKeyFile:         strings.TrimSpace(os.Getenv("MARKETING_RESEND_API_KEY_FILE")),
		ReviewRecipient:          strings.TrimSpace(os.Getenv("MARKETING_EARLY_ACCESS_REVIEW_RECIPIENT")),
		ReviewBaseURL:            strings.TrimSpace(os.Getenv("MARKETING_REVIEW_BASE_URL")),
		HostedSignupBaseURL:      strings.TrimSpace(os.Getenv("MYCOORIGYN_HOSTED_SIGNUP_BASE_URL")),
		TokenSecretRoot:          strings.TrimSpace(os.Getenv("MARKETING_TOKEN_SECRET_ROOT")),
	}

	if cfg.DatabaseURL == "" {
		dbCfg, err := LoadDatabaseConfig()
		if err != nil {
			return Config{}, err
		}
		cfg.DatabaseURL = dbCfg.DatabaseURL()
	}

	var err error
	if cfg.ReadTimeout, err = durationFromSecondsEnv("READ_TIMEOUT_SECONDS", defaultReadTimeoutSeconds); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = durationFromSecondsEnv("WRITE_TIMEOUT_SECONDS", defaultWriteTimeoutSeconds); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = durationFromSecondsEnv("IDLE_TIMEOUT_SECONDS", defaultIdleTimeoutSeconds); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = durationFromSecondsEnv("SHUTDOWN_TIMEOUT_SECONDS", defaultShutdownTimeoutSeconds); err != nil {
		return Config{}, err
	}
	if cfg.ReviewLifetime, err = durationFromSecondsEnv("MARKETING_REVIEW_TOKEN_TTL_SECONDS", 7*24*60*60); err != nil {
		return Config{}, err
	}
	if cfg.GrantLifetime, err = durationFromSecondsEnv("MARKETING_SIGNUP_GRANT_TTL_SECONDS", 7*24*60*60); err != nil {
		return Config{}, err
	}
	if cfg.ClaimLifetime, err = durationFromSecondsEnv("MARKETING_SIGNUP_GRANT_CLAIM_TTL_SECONDS", 30*60); err != nil {
		return Config{}, err
	}

	if cfg.TokenSecretRoot == "" && (environment == "development" || environment == "testing") {
		cfg.TokenSecretRoot = ".local/marketing-tokens"
	}
	if cfg.TokenSecretRoot == "" {
		return Config{}, errors.New("MARKETING_TOKEN_SECRET_ROOT is required")
	}
	if cfg.EmailProvider != "disabled" && cfg.EmailProvider != "resend" {
		return Config{}, errors.New("MARKETING_EMAIL_PROVIDER must be disabled or resend")
	}
	if cfg.EmailProvider == "resend" {
		if cfg.EmailFrom == "" || cfg.ResendAPIKeyFile == "" || cfg.ReviewRecipient == "" || cfg.ReviewBaseURL == "" || cfg.HostedSignupBaseURL == "" {
			return Config{}, errors.New("Resend email delivery requires sender, API-key file, reviewer recipient, review URL, and hosted signup URL")
		}
	}
	if cfg.EmailAllowedRecipients, err = normalizeEmailAddresses(cfg.EmailAllowedRecipients); err != nil {
		return Config{}, err
	}
	if environment == "staging" || environment == "production" {
		if cfg.EmailProvider != "resend" {
			return Config{}, errors.New("closed-alpha staging and production require MARKETING_EMAIL_PROVIDER=resend")
		}
		if err := requireHTTPSURL("MARKETING_REVIEW_BASE_URL", cfg.ReviewBaseURL); err != nil {
			return Config{}, err
		}
		if err := requireHTTPSURL("MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", cfg.HostedSignupBaseURL); err != nil {
			return Config{}, err
		}
		if environment == "staging" && len(cfg.EmailAllowedRecipients) == 0 {
			return Config{}, errors.New("MARKETING_EMAIL_ALLOWED_RECIPIENTS is required in staging")
		}
	}

	secretFile := strings.TrimSpace(os.Getenv("MARKETING_PROVISIONING_SHARED_SECRET_FILE"))
	if secretFile != "" {
		cfg.ProvisioningSecret, err = readPrivateSecret(secretFile)
		if err != nil {
			return Config{}, err
		}
	}
	if (environment == "staging" || environment == "production") && len(cfg.ProvisioningSecret) < 32 {
		return Config{}, errors.New("MARKETING_PROVISIONING_SHARED_SECRET_FILE must contain at least 32 bytes")
	}

	return cfg, nil
}

func normalizeEmailAddresses(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value || len(value) > 320 {
			return nil, errors.New("MARKETING_EMAIL_ALLOWED_RECIPIENTS must contain valid email addresses")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

func requireHTTPSURL(name, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an https URL without query or fragment", name)
	}
	return nil
}

func readPrivateSecret(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("read provisioning service authentication configuration")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("provisioning service secret file must be a private regular file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read provisioning service authentication configuration")
	}
	secret := []byte(strings.TrimSpace(string(body)))
	if len(secret) < 32 || strings.ContainsAny(string(secret), "\r\n") {
		return nil, errors.New("provisioning service secret must contain at least 32 bytes on one line")
	}
	return secret, nil
}

// LoadDatabaseConfig loads the database configuration from environment variables.
func LoadDatabaseConfig() (*DatabaseConfig, error) {
	dbName := strings.TrimSpace(os.Getenv("DB_NAME"))
	host := strings.TrimSpace(os.Getenv("DB_HOST"))
	password := os.Getenv("DB_PASSWORD")
	portStr := strings.TrimSpace(envOrDefault("DB_PORT", envOrDefault("POSTGRES_PORT", "5432")))
	sslMode := envOrDefault("DB_SSLMODE", "disable")
	user := strings.TrimSpace(os.Getenv("DB_USER"))

	if dbName == "" || host == "" || user == "" || portStr == "" || password == "" {
		return nil, errors.New("missing required database environment variables: set DATABASE_URL or DB_NAME, DB_HOST, DB_PORT, DB_USER, and DB_PASSWORD")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("DB_PORT must be a positive integer")
	}

	return &DatabaseConfig{
		DBName:   dbName,
		Host:     host,
		Password: password,
		Port:     port,
		SSLMode:  sslMode,
		User:     user,
	}, nil
}

func (c DatabaseConfig) DatabaseURL() string {
	return (&url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:   c.DBName,
		RawQuery: url.Values{
			"sslmode": []string{c.SSLMode},
		}.Encode(),
	}).String()
}

func (c Config) HTTPAddr() string {
	if strings.HasPrefix(c.Port, ":") {
		return c.Port
	}
	return ":" + c.Port
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func durationFromSecondsEnv(key string, fallback int) (time.Duration, error) {
	value := envOrDefault(key, strconv.Itoa(fallback))
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return time.Duration(seconds) * time.Second, nil
}
