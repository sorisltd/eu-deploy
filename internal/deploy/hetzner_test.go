package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSiteCaddyfile(t *testing.T) {
	got := renderSiteCaddyfile("example.com", "/", 3000)

	if !strings.Contains(got, "example.com {") {
		t.Fatalf("missing site block:\n%s", got)
	}
	if !strings.Contains(got, "reverse_proxy 127.0.0.1:3000") {
		t.Fatalf("missing reverse_proxy:\n%s", got)
	}
}

func TestRenderSiteCaddyfileWithRoutePath(t *testing.T) {
	got := renderSiteCaddyfile("example.com", "/book", 3002)

	if !strings.Contains(got, "handle_path /book* {") {
		t.Fatalf("missing handle_path block:\n%s", got)
	}
	if !strings.Contains(got, "respond 404") {
		t.Fatalf("missing 404 fallback:\n%s", got)
	}
}

func TestRenderRootCaddyfile(t *testing.T) {
	if got := renderRootCaddyfile(); got != "import /etc/caddy/sites/*.caddy\n" {
		t.Fatalf("unexpected root caddyfile: %q", got)
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

func TestBuildHetznerSiteConfigName(t *testing.T) {
	got := BuildHetznerSiteConfigName("massage.example.com")
	if got != "massage.example.com.caddy" {
		t.Fatalf("unexpected site config name: %s", got)
	}

	got = BuildHetznerSiteConfigName("bad/path:with spaces")
	if got != "bad-path-with-spaces.caddy" {
		t.Fatalf("unexpected sanitized site config name: %s", got)
	}
}

func TestRenderHetznerBootstrapScript(t *testing.T) {
	got := renderHetznerBootstrapScript(HetznerBootstrapOptions{
		RemoteServerPath: "/opt/eu-deploy",
		RemoteAppPath:    "/opt/eu-deploy/apps/massage",
		InstallUFW:       true,
		InstallFail2ban:  true,
	})

	for _, expected := range []string{
		"curl -fsSL https://get.docker.com | $SUDO sh",
		"$SUDO systemctl enable --now docker",
		"$SUDO mkdir -p '/opt/eu-deploy'",
		"$SUDO mkdir -p '/opt/eu-deploy/apps/massage'",
		"$SUDO mkdir -p '/opt/eu-deploy/_proxy/sites'",
		"$SUDO apt-get install -y ufw",
		"$SUDO apt-get install -y fail2ban",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("bootstrap script missing %q:\n%s", expected, got)
		}
	}
}

func TestInterpretSharedProxyCheck(t *testing.T) {
	if got := interpretSharedProxyCheck("proxy"); got.Status != PreflightOK {
		t.Fatalf("expected OK for proxy status, got %s", got.Status)
	}
	if got := interpretSharedProxyCheck("busy"); got.Status != PreflightFailure {
		t.Fatalf("expected failure for busy status, got %s", got.Status)
	}
}

func TestInterpretServicePortCheck(t *testing.T) {
	if got := interpretServicePortCheck("app-container", 3001); got.Status != PreflightOK {
		t.Fatalf("expected OK for existing app container, got %s", got.Status)
	}
	if got := interpretServicePortCheck("busy", 3001); got.Status != PreflightFailure {
		t.Fatalf("expected failure for busy service port, got %s", got.Status)
	}
}
