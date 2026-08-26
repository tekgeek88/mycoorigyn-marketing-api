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
	t.Setenv("MARKETING_PUBLIC_WEB_ORIGIN", "https://www.example.com")
	t.Setenv("MARKETING_REVIEW_BASE_URL", "https://www.example.com/early-access/review")
	t.Setenv("MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", "https://www.example.com/signup")
	t.Setenv("PUBLIC_CORS_ALLOWED_ORIGINS", "https://www.example.com")
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

func TestProductionDoesNotRequireEmailAllowlist(t *testing.T) {
	setBaseEnvironment(t)
	setProtectedEnvironment(t, "production")
	if _, err := Load(); err != nil {
		t.Fatalf("production compatibility changed: %v", err)
	}
}

func TestProtectedEnvironmentRequiresCanonicalWebOriginContract(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			setBaseEnvironment(t)
			setProtectedEnvironment(t, environment)

			for name, value := range map[string]string{
				"missing origin":           "",
				"origin with path":         "https://www.example.com/marketing",
				"origin with credentials":  "https://user@www.example.com",
				"origin with query":        "https://www.example.com?source=mail",
				"origin with empty query":  "https://www.example.com?",
				"origin with fragment":     "https://www.example.com#mail",
				"origin with encoded path": "https://www.example.com/%2e",
			} {
				t.Run(name, func(t *testing.T) {
					t.Setenv("MARKETING_PUBLIC_WEB_ORIGIN", value)
					if _, err := Load(); err == nil {
						t.Fatalf("expected %s rejection", name)
					}
				})
			}
		})
	}
}

func TestProtectedEnvironmentRejectsCrossOriginAndWrongCapabilityRoutes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "production review sent to staging", key: "MARKETING_REVIEW_BASE_URL", value: "https://staging.example.com/early-access/review"},
		{name: "production signup sent to staging", key: "MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", value: "https://staging.example.com/signup"},
		{name: "review wrong path", key: "MARKETING_REVIEW_BASE_URL", value: "https://www.example.com/signup"},
		{name: "signup wrong path", key: "MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", value: "https://www.example.com/early-access/review"},
		{name: "review userinfo", key: "MARKETING_REVIEW_BASE_URL", value: "https://user@www.example.com/early-access/review"},
		{name: "signup query", key: "MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", value: "https://www.example.com/signup?source=mail"},
		{name: "signup fragment", key: "MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", value: "https://www.example.com/signup#access=bad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setBaseEnvironment(t)
			setProtectedEnvironment(t, "production")
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatalf("expected %s rejection", tc.name)
			}
		})
	}
}

func TestStagingRejectsProductionCapabilityDestination(t *testing.T) {
	setBaseEnvironment(t)
	setProtectedEnvironment(t, "staging")
	t.Setenv("MARKETING_PUBLIC_WEB_ORIGIN", "https://staging.example.com")
	t.Setenv("PUBLIC_CORS_ALLOWED_ORIGINS", "https://staging.example.com")
	t.Setenv("MARKETING_REVIEW_BASE_URL", "https://staging.example.com/early-access/review")
	t.Setenv("MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", "https://www.example.com/signup")
	if _, err := Load(); err == nil {
		t.Fatal("expected staging-to-production signup destination rejection")
	}
}

func TestProtectedEnvironmentRequiresCanonicalOriginInCORS(t *testing.T) {
	setBaseEnvironment(t)
	setProtectedEnvironment(t, "production")
	t.Setenv("PUBLIC_CORS_ALLOWED_ORIGINS", "https://secondary.example.com")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PUBLIC_CORS_ALLOWED_ORIGINS") {
		t.Fatalf("canonical CORS error = %v", err)
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
		"MARKETING_RESEND_API_KEY_FILE", "MARKETING_EARLY_ACCESS_REVIEW_RECIPIENT",
		"MARKETING_PUBLIC_WEB_ORIGIN", "MARKETING_REVIEW_BASE_URL", "MYCOORIGYN_HOSTED_SIGNUP_BASE_URL",
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
	t.Setenv("MARKETING_PUBLIC_WEB_ORIGIN", "https://www.example.com")
	t.Setenv("MARKETING_REVIEW_BASE_URL", "https://www.example.com/early-access/review")
	t.Setenv("MYCOORIGYN_HOSTED_SIGNUP_BASE_URL", "https://www.example.com/signup")
	t.Setenv("PUBLIC_CORS_ALLOWED_ORIGINS", "https://www.example.com")
	t.Setenv("MARKETING_TOKEN_SECRET_ROOT", "/private/token-root")
	secretPath := filepath.Join(t.TempDir(), "provisioning-secret")
	if err := os.WriteFile(secretPath, []byte(strings.Repeat("s", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MARKETING_PROVISIONING_SHARED_SECRET_FILE", secretPath)
}
