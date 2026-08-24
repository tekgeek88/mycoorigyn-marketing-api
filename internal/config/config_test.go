package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentDefaultsKeepEmailDisabled(t *testing.T) {
	setBaseEnvironment(t)
	t.Setenv("ENV", "development")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmailProvider != "disabled" || cfg.TokenSecretRoot != ".local/marketing-tokens" {
		t.Fatalf("unexpected development defaults: %#v", cfg)
	}
}

func TestProductionFailsClosedWithoutProtectedEmailAndServiceConfig(t *testing.T) {
	setBaseEnvironment(t)
	t.Setenv("ENV", "production")
	if _, err := Load(); err == nil {
		t.Fatal("expected production configuration failure")
	}
}

func TestProductionLoadsPrivateProvisioningSecret(t *testing.T) {
	setBaseEnvironment(t)
	t.Setenv("ENV", "production")
	t.Setenv("MARKETING_EMAIL_PROVIDER", "resend")
	t.Setenv("MARKETING_EMAIL_FROM", "MycoOrigyn <notify@example.com>")
	t.Setenv("MARKETING_RESEND_API_KEY_FILE", "/private/resend-key")
	t.Setenv("MARKETING_EARLY_ACCESS_REVIEW_RECIPIENT", "reviewer@example.com")
	t.Setenv("MARKETING_REVIEW_BASE_URL", "https://www.example.com/early-access/review")
	t.Setenv("MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", "https://app.example.com/signup")
	t.Setenv("MARKETING_TOKEN_SECRET_ROOT", "/private/token-root")
	secretPath := filepath.Join(t.TempDir(), "provisioning-secret")
	secret := strings.Repeat("s", 32)
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MARKETING_PROVISIONING_SHARED_SECRET_FILE", secretPath)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.ProvisioningSecret) != secret {
		t.Fatal("provisioning secret did not load")
	}
}

func TestStagingRequiresAndNormalizesEmailAllowlist(t *testing.T) {
	setBaseEnvironment(t)
	setProtectedEnvironment(t, "staging")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MARKETING_EMAIL_ALLOWED_RECIPIENTS") {
		t.Fatalf("missing staging allowlist error = %v", err)
	}

	t.Setenv("MARKETING_EMAIL_ALLOWED_RECIPIENTS", "Reviewer@Example.com,tester@example.com,reviewer@example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(cfg.EmailAllowedRecipients, ","), "reviewer@example.com,tester@example.com"; got != want {
		t.Fatalf("allowed recipients = %q, want %q", got, want)
	}
}

func TestProductionDoesNotRequireEmailAllowlist(t *testing.T) {
	setBaseEnvironment(t)
	setProtectedEnvironment(t, "production")
	if _, err := Load(); err != nil {
		t.Fatalf("production compatibility changed: %v", err)
	}
}

func TestProvisioningSecretFileMustBePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(strings.Repeat("s", 32)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateSecret(path); err == nil {
		t.Fatal("expected unsafe secret file rejection")
	}
}

func setBaseEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost/database?sslmode=disable")
	for _, key := range []string{
		"MARKETING_EMAIL_PROVIDER", "MARKETING_EMAIL_FROM", "MARKETING_EMAIL_REPLY_TO",
		"MARKETING_EMAIL_ALLOWED_RECIPIENTS",
		"MARKETING_RESEND_API_KEY_FILE", "MARKETING_EARLY_ACCESS_REVIEW_RECIPIENT",
		"MARKETING_REVIEW_BASE_URL", "MYCOORIGYN_HOSTED_SIGNUP_BASE_URL",
		"MARKETING_TOKEN_SECRET_ROOT", "MARKETING_PROVISIONING_SHARED_SECRET_FILE",
	} {
		t.Setenv(key, "")
	}
}

func setProtectedEnvironment(t *testing.T, environment string) {
	t.Helper()
	t.Setenv("ENV", environment)
	t.Setenv("MARKETING_EMAIL_PROVIDER", "resend")
	t.Setenv("MARKETING_EMAIL_FROM", "MycoOrigyn <notify@example.com>")
	t.Setenv("MARKETING_RESEND_API_KEY_FILE", "/private/resend-key")
	t.Setenv("MARKETING_EARLY_ACCESS_REVIEW_RECIPIENT", "reviewer@example.com")
	t.Setenv("MARKETING_REVIEW_BASE_URL", "https://www.example.com/early-access/review")
	t.Setenv("MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", "https://app.example.com/signup")
	t.Setenv("MARKETING_TOKEN_SECRET_ROOT", "/private/token-root")
	secretPath := filepath.Join(t.TempDir(), "provisioning-secret")
	if err := os.WriteFile(secretPath, []byte(strings.Repeat("s", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MARKETING_PROVISIONING_SHARED_SECRET_FILE", secretPath)
}
