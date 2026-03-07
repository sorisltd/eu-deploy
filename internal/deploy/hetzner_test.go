package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderCaddyfile(t *testing.T) {
	got := renderCaddyfile("example.com", "/", 3000)

	if !strings.Contains(got, "example.com {") {
		t.Fatalf("missing site block:\n%s", got)
	}
	if !strings.Contains(got, "reverse_proxy 127.0.0.1:3000") {
		t.Fatalf("missing reverse_proxy:\n%s", got)
	}
}

func TestRenderEnvFileRejectsNewlines(t *testing.T) {
	_, err := renderEnvFile(map[string]string{
		"JWT_SECRET": "line1\nline2",
	})
	if err == nil {
		t.Fatalf("expected newline validation error")
	}
}

func TestParseEnvTemplateKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.example")
	if err := os.WriteFile(path, []byte("# comment\nDATABASE_URL=value\nJWT_SECRET=\nDATABASE_URL=duplicate\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	keys, err := parseEnvTemplateKeys(path)
	if err != nil {
		t.Fatalf("parseEnvTemplateKeys: %v", err)
	}

	if strings.Join(keys, ",") != "DATABASE_URL,JWT_SECRET" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}
