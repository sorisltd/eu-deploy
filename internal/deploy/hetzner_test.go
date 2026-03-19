package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSiteCaddyfile(t *testing.T) {
	got := renderSiteCaddyfile([]string{"example.com"}, "/", 3000, "example-app")

	if !strings.Contains(got, "example.com {") {
		t.Fatalf("missing site block:\n%s", got)
	}
	if !strings.Contains(got, "reverse_proxy 127.0.0.1:3000") {
		t.Fatalf("missing reverse_proxy:\n%s", got)
	}
	if !strings.Contains(got, "output file /var/log/caddy/example-app.access.log") {
		t.Fatalf("missing analytics log output:\n%s", got)
	}
}

func TestRenderSiteCaddyfileWithRoutePath(t *testing.T) {
	got := renderSiteCaddyfile([]string{"example.com"}, "/book", 3002, "example-app")

	if !strings.Contains(got, "handle_path /book* {") {
		t.Fatalf("missing handle_path block:\n%s", got)
	}
	if !strings.Contains(got, "respond 404") {
		t.Fatalf("missing 404 fallback:\n%s", got)
	}
}

func TestRenderSiteCaddyfileWithMultipleHostnames(t *testing.T) {
	got := renderSiteCaddyfile([]string{"example.com", "www.example.com"}, "/", 3000, "example-app")

	if !strings.Contains(got, "example.com, www.example.com {") {
		t.Fatalf("missing combined site block:\n%s", got)
	}
	if !strings.Contains(got, "reverse_proxy 127.0.0.1:3000") {
		t.Fatalf("missing reverse_proxy:\n%s", got)
	}
}

func TestRenderSiteCaddyfileWithIPAddressUsesHTTP(t *testing.T) {
	got := renderSiteCaddyfile([]string{"89.167.126.100"}, "/", 3000, "example-app")

	if !strings.Contains(got, "http://89.167.126.100 {") {
		t.Fatalf("ip host should be rendered as an http site block:\n%s", got)
	}
	if strings.Contains(got, "\n89.167.126.100 {") {
		t.Fatalf("ip host should not use a bare host label that triggers auto-https:\n%s", got)
	}
}

func TestRenderStaticSiteCaddyfile(t *testing.T) {
	got := renderStaticSiteCaddyfile([]string{"example.com"}, "/", "/opt/eu-deploy/apps/example/static", "example-app")

	for _, expected := range []string{
		"example.com {",
		"output file /var/log/caddy/example-app.access.log",
		"root * /opt/eu-deploy/apps/example/static",
		"try_files {path} /index.html",
		"file_server",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("static site caddyfile missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderStaticSiteCaddyfileWithIPAddressUsesHTTP(t *testing.T) {
	got := renderStaticSiteCaddyfile([]string{"89.167.126.100"}, "/", "/opt/eu-deploy/apps/example/static", "example-app")

	if !strings.Contains(got, "http://89.167.126.100 {") {
		t.Fatalf("ip host should be rendered as an http site block:\n%s", got)
	}
}

func TestRenderStaticSiteCaddyfileWithRoutePath(t *testing.T) {
	got := renderStaticSiteCaddyfile([]string{"example.com"}, "/docs", "/opt/eu-deploy/apps/example/static", "example-app")

	if !strings.Contains(got, "handle_path /docs* {") {
		t.Fatalf("missing static handle_path block:\n%s", got)
	}
	if !strings.Contains(got, "respond 404") {
		t.Fatalf("missing static 404 fallback:\n%s", got)
	}
}

func TestRenderMaintenanceSiteCaddyfile(t *testing.T) {
	got := renderMaintenanceSiteCaddyfile([]string{"example.com"}, "/", "/opt/eu-deploy/apps/example/maintenance")

	for _, expected := range []string{
		"example.com {",
		`header Cache-Control "no-store"`,
		"root * /opt/eu-deploy/apps/example/maintenance",
		"try_files {path} /index.html",
		"file_server",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("maintenance caddyfile missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "output file /var/log/caddy") {
		t.Fatalf("maintenance caddyfile should not write analytics logs:\n%s", got)
	}
}

func TestRenderMaintenanceSiteCaddyfileWithIPAddressUsesHTTP(t *testing.T) {
	got := renderMaintenanceSiteCaddyfile([]string{"89.167.126.100"}, "/", "/opt/eu-deploy/apps/example/maintenance")

	if !strings.Contains(got, "http://89.167.126.100 {") {
		t.Fatalf("ip host should be rendered as an http site block:\n%s", got)
	}
}

func TestRenderRootCaddyfile(t *testing.T) {
	got := renderRootCaddyfile()

	for _, expected := range []string{
		"trusted_proxies static 173.245.48.0/20",
		"2c0f:f248::/32",
		"client_ip_headers CF-Connecting-IP",
		"trusted_proxies_strict",
		"import /etc/caddy/sites/*.caddy",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("root caddyfile missing %q:\n%s", expected, got)
		}
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
		SharedDatabase: &SharedDatabaseOptions{
			Version: "16",
			Name:    "massage",
			User:    "massage",
		},
	})

	for _, expected := range []string{
		"curl -fsSL https://get.docker.com | $SUDO sh",
		"$SUDO systemctl enable --now docker",
		"$SUDO mkdir -p '/opt/eu-deploy'",
		"$SUDO mkdir -p '/opt/eu-deploy/apps/massage'",
		"$SUDO mkdir -p '/opt/eu-deploy/_proxy/sites'",
		"$SUDO mkdir -p '/var/log/caddy'",
		"$SUDO docker network inspect 'eu-deploy' >/dev/null 2>&1 || $SUDO docker network create 'eu-deploy' >/dev/null",
		"$SUDO docker run -d --restart unless-stopped --network 'eu-deploy' -p 127.0.0.1:5432:5432 --name 'eu-shared-postgres'",
		"$SUDO apt-get install -y ufw",
		"$SUDO apt-get install -y fail2ban",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("bootstrap script missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderHetznerDeployScriptRunsPostDeployCommand(t *testing.T) {
	got := renderHetznerDeployScript(HetznerOptions{
		RemoteServerPath:   "/opt/eu-deploy",
		RemoteAppPath:      "/opt/eu-deploy/apps/massage",
		RemoteHost:         "example.com",
		RemoteUser:         "root",
		RemotePort:         22,
		RuntimeStart:       "npm run start",
		ContainerPort:      3000,
		ServicePort:        3001,
		ImageTag:           "eu-deploy-massage:remote",
		ReleaseID:          "release-123",
		AppContainerName:   "eu-massage-app",
		ProxyContainerName: "eu-shared-caddy",
		Hostname:           "massage.example.com",
		RoutePath:          "/",
		HealthcheckPath:    "/",
		SiteConfigName:     "massage.example.com.caddy",
		PostDeploy: &PostDeployOptions{
			Command: "node scripts/setup-db.js",
		},
	})

	if !strings.Contains(got, `docker exec "$next_container" bash -lc 'node scripts/setup-db.js'`) {
		t.Fatalf("deploy script should run the configured post-deploy command:\n%s", got)
	}
	if !strings.Contains(got, `docker exec 'eu-shared-caddy' caddy reload --config /etc/caddy/Caddyfile`) {
		t.Fatalf("deploy script should reload the shared proxy instead of only recreating it:\n%s", got)
	}
	if !strings.Contains(got, `-v '/var/log/caddy':/var/log/caddy`) {
		t.Fatalf("deploy script should mount the host caddy log directory:\n%s", got)
	}
	if !strings.Contains(got, `-v '/opt/eu-deploy':'/opt/eu-deploy':ro`) {
		t.Fatalf("deploy script should mount the remote server root for maintenance/static assets:\n%s", got)
	}
}

func TestRenderHetznerDeployScriptCopiesPackageMetadataIntoReleaseDir(t *testing.T) {
	got := renderHetznerDeployScript(HetznerOptions{
		RemoteServerPath:    "/opt/eu-deploy",
		RemoteAppPath:       "/opt/eu-deploy/apps/massage",
		RemoteHost:          "example.com",
		RemoteUser:          "root",
		RemotePort:          22,
		RuntimeStart:        "npm run start",
		ContainerPort:       3000,
		ServicePort:         3001,
		ImageTag:            "eu-deploy-massage:remote",
		ReleaseID:           "release-123",
		AppContainerName:    "eu-massage-app",
		ProxyContainerName:  "eu-shared-caddy",
		Hostname:            "massage.example.com",
		RoutePath:           "/",
		HealthcheckPath:     "/",
		SiteConfigName:      "massage.example.com.caddy",
		InstallDependencies: true,
	})

	for _, expected := range []string{
		`if [ -f package.json ]; then cp package.json '/opt/eu-deploy/apps/massage/releases/release-123/package.json'; fi`,
		`if [ -f package-lock.json ]; then cp package-lock.json '/opt/eu-deploy/apps/massage/releases/release-123/package-lock.json'; fi`,
		`-v '/var/log/caddy':/var/log/caddy`,
		`-v '/opt/eu-deploy':'/opt/eu-deploy':ro`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("deploy script missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderHetznerDeployScriptMigratesLegacySingleContainer(t *testing.T) {
	got := renderHetznerDeployScript(HetznerOptions{
		RemoteServerPath:    "/opt/eu-deploy",
		RemoteAppPath:       "/opt/eu-deploy/apps/massage",
		RemoteHost:          "example.com",
		RemoteUser:          "root",
		RemotePort:          22,
		RuntimeStart:        "npm run start",
		ContainerPort:       3000,
		ServicePort:         3001,
		ImageTag:            "eu-deploy-massage:remote",
		ReleaseID:           "release-123",
		AppContainerName:    "eu-massage-app",
		ProxyContainerName:  "eu-shared-caddy",
		Hostname:            "massage.example.com",
		RoutePath:           "/",
		HealthcheckPath:     "/",
		SiteConfigName:      "massage.example.com.caddy",
		InstallDependencies: true,
	})

	for _, expected := range []string{
		`legacy_container="$APP_CONTAINER_BASE"`,
		`if [ -z "$active_slot" ] && docker ps --format '{{.Names}}' | grep -Fx -- "$legacy_container" >/dev/null 2>&1; then`,
		`  active_slot='a'`,
		`  docker rm -f "$legacy_container" >/dev/null 2>&1 || true`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("deploy script missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderHetznerDeployScriptPreservesMaintenanceMode(t *testing.T) {
	got := renderHetznerDeployScript(HetznerOptions{
		RemoteServerPath:   "/opt/eu-deploy",
		RemoteAppPath:      "/opt/eu-deploy/apps/massage",
		RemoteHost:         "example.com",
		RemoteUser:         "root",
		RemotePort:         22,
		RuntimeStart:       "npm run start",
		ContainerPort:      3000,
		ServicePort:        3001,
		ImageTag:           "eu-deploy-massage:remote",
		ReleaseID:          "release-123",
		AppContainerName:   "eu-massage-app",
		ProxyContainerName: "eu-shared-caddy",
		Hostname:           "massage.example.com",
		Hostnames:          []string{"massage.example.com"},
		RoutePath:          "/",
		HealthcheckPath:    "/",
		SiteConfigName:     "massage.example.com.caddy",
	})

	for _, expected := range []string{
		`if [ -f '/opt/eu-deploy/apps/massage/maintenance.json' ] && grep -Eq '"enabled"[[:space:]]*:[[:space:]]*true' '/opt/eu-deploy/apps/massage/maintenance.json'; then`,
		`if [ ! -f '/opt/eu-deploy/apps/massage/maintenance/index.html' ]; then`,
		`root * /opt/eu-deploy/apps/massage/maintenance`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("deploy script missing maintenance preservation %q:\n%s", expected, got)
		}
	}
}

func TestRenderStaticHetznerDeployScript(t *testing.T) {
	got := renderHetznerDeployScript(HetznerOptions{
		RuntimeType:        "static",
		RemoteServerPath:   "/opt/eu-deploy",
		RemoteAppPath:      "/opt/eu-deploy/apps/example",
		RemoteHost:         "example.com",
		RemoteUser:         "root",
		RemotePort:         22,
		ReleaseID:          "release-123",
		ArtifactSHA:        "sha123",
		StaticArchiveRoot:  "dist",
		ProxyContainerName: "eu-shared-caddy",
		Hostname:           "example.com",
		Hostnames:          []string{"example.com"},
		RoutePath:          "/",
		SiteConfigName:     "example.com.caddy",
		ImageTag:           "eu-deploy-example:remote",
	})

	for _, expected := range []string{
		"tar -xzf '/opt/eu-deploy/apps/example/releases/release-123/artifact.tar.gz' -C '/opt/eu-deploy/apps/example/releases/release-123'",
		"ln -sfn '/opt/eu-deploy/apps/example/releases/release-123/dist' '/opt/eu-deploy/apps/example/static'",
		"root * /opt/eu-deploy/apps/example/static",
		"try_files {path} /index.html",
		"printf '%s\\t%s\\t%s\\t%s\\t%s\\t%s\\n' 'release-123' 'static' '0'",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("static deploy script missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "docker build -t") {
		t.Fatalf("static deploy script should not build a docker image:\n%s", got)
	}
}

func TestRenderEnableRemoteMaintenanceScript(t *testing.T) {
	got := renderEnableRemoteMaintenanceScript(HetznerOptions{
		RuntimeType:        "web",
		ProjectName:        "massage",
		RemoteServerPath:   "/opt/eu-deploy",
		RemoteAppPath:      "/opt/eu-deploy/apps/massage",
		RemoteHost:         "example.com",
		RemoteUser:         "root",
		RemotePort:         22,
		ServicePort:        3001,
		ProxyContainerName: "eu-shared-caddy",
		Hostname:           "massage.example.com",
		Hostnames:          []string{"massage.example.com"},
		RoutePath:          "/",
		SiteConfigName:     "massage.example.com.caddy",
	}, "Back in 30 minutes.")

	for _, expected := range []string{
		`cat > '/opt/eu-deploy/apps/massage/maintenance/index.html' <<'EOF'`,
		`Back in 30 minutes.`,
		`cat > '/opt/eu-deploy/apps/massage/maintenance.json' <<EOF`,
		`"enabled": true`,
		`"message": "Back in 30 minutes."`,
		`root * /opt/eu-deploy/apps/massage/maintenance`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("enable maintenance script missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderDisableRemoteMaintenanceScriptRestoresLiveSite(t *testing.T) {
	got := renderDisableRemoteMaintenanceScript(HetznerOptions{
		RuntimeType:        "web",
		RemoteServerPath:   "/opt/eu-deploy",
		RemoteAppPath:      "/opt/eu-deploy/apps/massage",
		RemoteHost:         "example.com",
		RemoteUser:         "root",
		RemotePort:         22,
		ServicePort:        3001,
		ProxyContainerName: "eu-shared-caddy",
		Hostname:           "massage.example.com",
		Hostnames:          []string{"massage.example.com"},
		RoutePath:          "/",
		SiteConfigName:     "massage.example.com.caddy",
		AnalyticsLogName:   "massage",
	})

	for _, expected := range []string{
		`rm -f '/opt/eu-deploy/apps/massage/maintenance.json'`,
		`rm -rf '/opt/eu-deploy/apps/massage/maintenance'`,
		`PRIMARY_PORT=3001`,
		`SECONDARY_PORT=3002`,
		`if [ "$active_slot" = 'b' ]; then`,
		`TARGET_PORT=$SECONDARY_PORT`,
		`grep -Fx -- '/opt/eu-deploy -> /opt/eu-deploy'`,
		`output file /var/log/caddy/massage.access.log`,
		`reverse_proxy 127.0.0.1:${TARGET_PORT}`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("disable maintenance script missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderStaticRollbackRemoteScript(t *testing.T) {
	got := renderRollbackRemoteScript(HetznerOptions{
		RuntimeType:        "static",
		RemoteServerPath:   "/opt/eu-deploy",
		RemoteAppPath:      "/opt/eu-deploy/apps/example",
		RemoteHost:         "example.com",
		RemoteUser:         "root",
		RemotePort:         22,
		StaticArchiveRoot:  "dist",
		ProxyContainerName: "eu-shared-caddy",
		Hostname:           "example.com",
		Hostnames:          []string{"example.com"},
		RoutePath:          "/",
		SiteConfigName:     "example.com.caddy",
		ImageTag:           "eu-deploy-example:remote",
	}, ReleaseRecord{
		ID:          "release-122",
		ArtifactSHA: "sha122",
	})

	for _, expected := range []string{
		"TARGET_ROOT='/opt/eu-deploy/apps/example/releases/release-122/dist'",
		"ln -sfn \"$TARGET_ROOT\" '/opt/eu-deploy/apps/example/static'",
		"root * /opt/eu-deploy/apps/example/static",
		"try_files {path} /index.html",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("static rollback script missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "docker run -d --restart unless-stopped --network 'eu-deploy'") {
		t.Fatalf("static rollback script should not start an app container:\n%s", got)
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

func TestInterpretSharedNetworkCheck(t *testing.T) {
	if got := interpretSharedNetworkCheck("present"); got.Status != PreflightOK {
		t.Fatalf("expected OK for existing shared network, got %s", got.Status)
	}
	if got := interpretSharedNetworkCheck("missing"); got.Status != PreflightWarning {
		t.Fatalf("expected warning for missing shared network, got %s", got.Status)
	}
}

func TestInterpretSharedPostgresCheck(t *testing.T) {
	if got := interpretSharedPostgresCheck("postgres"); got.Status != PreflightOK {
		t.Fatalf("expected OK for running shared postgres, got %s", got.Status)
	}
	if got := interpretSharedPostgresCheck("missing"); got.Status != PreflightWarning {
		t.Fatalf("expected warning for missing shared postgres, got %s", got.Status)
	}
	if got := interpretSharedPostgresCheck("busy"); got.Status != PreflightFailure {
		t.Fatalf("expected failure for busy postgres port, got %s", got.Status)
	}
}

func TestSanitizePostgresIdentifier(t *testing.T) {
	if got := sanitizePostgresIdentifier("Masazo Terapija"); got != "masazo_terapija" {
		t.Fatalf("unexpected postgres identifier: %s", got)
	}
	if got := sanitizePostgresIdentifier("123-demo"); got != "app_123_demo" {
		t.Fatalf("unexpected postgres identifier with numeric prefix: %s", got)
	}
}

func TestRenderSharedDatabaseSQL(t *testing.T) {
	got := renderSharedDatabaseSQL(SharedDatabaseOptions{
		Name: "masazo_terapija",
		User: "masazo_terapija",
	})

	for _, expected := range []string{
		`SELECT set_config('eu_deploy.app_password', :'db_password', false);`,
		`CREATE ROLE "masazo_terapija" LOGIN PASSWORD `,
		`ALTER ROLE "masazo_terapija" WITH LOGIN PASSWORD `,
		`CREATE DATABASE "masazo_terapija" OWNER "masazo_terapija"`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("shared database SQL missing %q:\n%s", expected, got)
		}
	}
}
