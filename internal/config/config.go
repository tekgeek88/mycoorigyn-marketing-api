package config

import (
	"errors"
	"fmt"
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
	Port                     string
	DatabaseURL              string
	PublicCORSAllowedOrigins []string
	ReadTimeout              time.Duration
	WriteTimeout             time.Duration
	IdleTimeout              time.Duration
	ShutdownTimeout          time.Duration
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
	cfg := Config{
		Port:                     envOrDefault("PORT", defaultPort),
		DatabaseURL:              strings.TrimSpace(os.Getenv("DATABASE_URL")),
		PublicCORSAllowedOrigins: splitCSV(envOrDefault("PUBLIC_CORS_ALLOWED_ORIGINS", defaultCORSAllowedOrigins)),
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

	return cfg, nil
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
