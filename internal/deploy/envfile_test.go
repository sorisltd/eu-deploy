package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLocalDeployEnvFilesPrefersDeployOverrides(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("JWT_SECRET=local-secret\nADMIN_NAME=\"Admin User\"\n"), 0o644); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.deploy.local"), []byte("JWT_SECRET=deploy-secret\nBOOKING_NOTIFY_TO='ops@example.com'\n"), 0o644); err != nil {
		t.Fatalf("write .env.deploy.local: %v", err)
	}

	values, err := LoadLocalDeployEnvFiles(dir)
	if err != nil {
		t.Fatalf("LoadLocalDeployEnvFiles: %v", err)
	}

	if got := values["JWT_SECRET"]; got != "deploy-secret" {
		t.Fatalf("JWT_SECRET = %q, want deploy override", got)
	}
	if got := values["ADMIN_NAME"]; got != "Admin User" {
		t.Fatalf("ADMIN_NAME = %q, want quoted value", got)
	}
	if got := values["BOOKING_NOTIFY_TO"]; got != "ops@example.com" {
		t.Fatalf("BOOKING_NOTIFY_TO = %q, want single-quoted value", got)
	}
}

func TestLoadLocalDeployEnvFilesIgnoresMissingFiles(t *testing.T) {
	values, err := LoadLocalDeployEnvFiles(t.TempDir())
	if err != nil {
		t.Fatalf("LoadLocalDeployEnvFiles: %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("expected no env values, got %v", values)
	}
}
