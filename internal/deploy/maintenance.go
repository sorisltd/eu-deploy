package deploy

import (
	"encoding/json"
	"fmt"
	"html"
	"path/filepath"
	"strings"
)

type MaintenanceStatus struct {
	Enabled   bool   `json:"enabled"`
	EnabledAt string `json:"enabledAt,omitempty"`
	Message   string `json:"message,omitempty"`
}

func EnableRemoteMaintenance(opts RemoteOptions, message string) (MaintenanceStatus, error) {
	if err := validateRemoteOptions(opts); err != nil {
		return MaintenanceStatus{}, err
	}
	if err := runRemoteScript(opts, renderEnableRemoteMaintenanceScript(opts, message)); err != nil {
		return MaintenanceStatus{}, err
	}
	return ReadRemoteMaintenanceStatus(opts)
}

func DisableRemoteMaintenance(opts RemoteOptions) (MaintenanceStatus, error) {
	if err := validateRemoteOptions(opts); err != nil {
		return MaintenanceStatus{}, err
	}
	if err := runRemoteScript(opts, renderDisableRemoteMaintenanceScript(opts)); err != nil {
		return MaintenanceStatus{}, err
	}
	return ReadRemoteMaintenanceStatus(opts)
}

func ReadRemoteMaintenanceStatus(opts RemoteOptions) (MaintenanceStatus, error) {
	if err := validateRemoteOptions(opts); err != nil {
		return MaintenanceStatus{}, err
	}

	out, err := runRemoteCommandCapture(opts, true, renderRemoteMaintenanceStatusCommand(opts))
	if err != nil {
		return MaintenanceStatus{}, err
	}

	var status MaintenanceStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &status); err != nil {
		return MaintenanceStatus{}, fmt.Errorf("parse maintenance status: %w", err)
	}
	return status, nil
}

func renderEnableRemoteMaintenanceScript(opts RemoteOptions, message string) string {
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	proxySitesDir := filepath.ToSlash(filepath.Join(proxyRoot, "sites"))
	rootCaddyPath := filepath.ToSlash(filepath.Join(proxyRoot, "Caddyfile"))
	proxyDataPath := filepath.ToSlash(filepath.Join(proxyRoot, "data"))
	proxyConfigPath := filepath.ToSlash(filepath.Join(proxyRoot, "config"))
	siteConfigPath := filepath.ToSlash(filepath.Join(proxySitesDir, opts.SiteConfigName))
	maintenanceRoot := maintenanceRootPath(opts)
	maintenanceIndex := maintenanceIndexPath(opts)
	statePath := maintenanceStatePath(opts)
	siteCaddy := renderMaintenanceSiteCaddyfile(opts.Hostnames, opts.RoutePath, maintenanceRoot)
	messageJSON, _ := json.Marshal(strings.TrimSpace(message))

	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("mkdir -p %s", shellQuote(maintenanceRoot)),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(maintenanceIndex), renderMaintenanceHTML(opts.ProjectName, message)),
		fmt.Sprintf("cat > %s <<EOF\n{\n  \"enabled\": true,\n  \"enabledAt\": \"$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)\",\n  \"message\": %s\n}\nEOF", shellQuote(statePath), string(messageJSON)),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(rootCaddyPath), renderRootCaddyfile()),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(siteConfigPath), siteCaddy),
	}
	lines = append(lines, renderProxyReloadCommands(opts, rootCaddyPath, proxySitesDir, proxyDataPath, proxyConfigPath)...)

	return strings.Join(lines, "\n")
}

func renderDisableRemoteMaintenanceScript(opts RemoteOptions) string {
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	proxySitesDir := filepath.ToSlash(filepath.Join(proxyRoot, "sites"))
	rootCaddyPath := filepath.ToSlash(filepath.Join(proxyRoot, "Caddyfile"))
	proxyDataPath := filepath.ToSlash(filepath.Join(proxyRoot, "data"))
	proxyConfigPath := filepath.ToSlash(filepath.Join(proxyRoot, "config"))
	siteConfigPath := filepath.ToSlash(filepath.Join(proxySitesDir, opts.SiteConfigName))
	statePath := maintenanceStatePath(opts)
	maintenanceRoot := maintenanceRootPath(opts)

	var siteCaddy string
	if isStaticRuntime(opts.RuntimeType) {
		siteCaddy = renderStaticSiteCaddyfile(opts.Hostnames, opts.RoutePath, staticCurrentRootPath(opts), opts.AnalyticsLogName)
	} else {
		siteCaddy = renderSiteCaddyfile(opts.Hostnames, opts.RoutePath, opts.ServicePort, opts.AnalyticsLogName)
	}

	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("rm -f %s", shellQuote(statePath)),
		fmt.Sprintf("rm -rf %s", shellQuote(maintenanceRoot)),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(rootCaddyPath), renderRootCaddyfile()),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(siteConfigPath), siteCaddy),
	}
	lines = append(lines, renderProxyReloadCommands(opts, rootCaddyPath, proxySitesDir, proxyDataPath, proxyConfigPath)...)

	return strings.Join(lines, "\n")
}

func renderRemoteMaintenanceStatusCommand(opts RemoteOptions) string {
	statePath := maintenanceStatePath(opts)
	return fmt.Sprintf("if [ -f %s ]; then cat %s; else printf '%%s\\n' '{\"enabled\":false}'; fi", shellQuote(statePath), shellQuote(statePath))
}

func renderMaintenanceAwareSiteConfigCommands(opts RemoteOptions, siteConfigPath, liveSiteCaddy string) []string {
	maintenanceRoot := maintenanceRootPath(opts)
	maintenanceIndex := maintenanceIndexPath(opts)
	statePath := maintenanceStatePath(opts)
	maintenanceCaddy := renderMaintenanceSiteCaddyfile(opts.Hostnames, opts.RoutePath, maintenanceRoot)

	return []string{
		fmt.Sprintf("if [ -f %s ] && grep -Eq '\"enabled\"[[:space:]]*:[[:space:]]*true' %s; then", shellQuote(statePath), shellQuote(statePath)),
		fmt.Sprintf("  mkdir -p %s", shellQuote(maintenanceRoot)),
		fmt.Sprintf("  if [ ! -f %s ]; then", shellQuote(maintenanceIndex)),
		fmt.Sprintf("    cat > %s <<'EOF'\n%sEOF", shellQuote(maintenanceIndex), renderMaintenanceHTML(opts.ProjectName, "")),
		"  fi",
		fmt.Sprintf("  cat > %s <<'EOF'\n%sEOF", shellQuote(siteConfigPath), maintenanceCaddy),
		"else",
		fmt.Sprintf("  cat > %s <<EOF\n%sEOF", shellQuote(siteConfigPath), liveSiteCaddy),
		"fi",
	}
}

func renderMaintenanceSiteCaddyfile(hostnames []string, routePath, rootPath string) string {
	hostLabel := formatCaddySiteHosts(hostnames)
	routePath = normalizeRoutePath(routePath)
	rootPath = strings.TrimSpace(rootPath)

	lines := []string{
		fmt.Sprintf("%s {", hostLabel),
		"  encode zstd gzip",
		`  header Cache-Control "no-store"`,
	}

	if routePath == "/" {
		lines = append(lines,
			fmt.Sprintf("  root * %s", rootPath),
			"  try_files {path} /index.html",
			"  file_server",
		)
	} else {
		lines = append(lines,
			fmt.Sprintf("  handle_path %s* {", routePath),
			`    header Cache-Control "no-store"`,
			fmt.Sprintf("    root * %s", rootPath),
			"    try_files {path} /index.html",
			"    file_server",
			"  }",
			"  respond 404",
		)
	}
	lines = append(lines, "}")

	return strings.Join(lines, "\n") + "\n"
}

func renderMaintenanceHTML(projectName, message string) string {
	title := "Temporarily unavailable"
	if name := strings.TrimSpace(projectName); name != "" {
		title = fmt.Sprintf("%s is temporarily unavailable", name)
	}

	body := "This site is temporarily offline for maintenance. Please check back soon."
	if trimmed := strings.TrimSpace(message); trimmed != "" {
		body = trimmed
	}

	return strings.Join([]string{
		"<!doctype html>",
		"<html lang=\"en\">",
		"<head>",
		"  <meta charset=\"utf-8\">",
		"  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">",
		fmt.Sprintf("  <title>%s</title>", html.EscapeString(title)),
		"  <style>",
		"    :root { color-scheme: dark; }",
		"    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: radial-gradient(circle at top, #12304a, #071018 62%); color: #f3f7fb; font-family: Georgia, 'Times New Roman', serif; }",
		"    main { width: min(92vw, 42rem); padding: 3rem; border: 1px solid rgba(255,255,255,0.14); border-radius: 1.5rem; background: rgba(7, 16, 24, 0.84); box-shadow: 0 20px 60px rgba(0,0,0,0.35); }",
		"    p.eyebrow { margin: 0; letter-spacing: 0.35em; text-transform: uppercase; font: 600 0.78rem/1.2 system-ui, sans-serif; color: #7dd3fc; }",
		"    h1 { margin: 1rem 0 0; font-size: clamp(2rem, 5vw, 3.3rem); line-height: 1.05; }",
		"    p.copy { margin: 1.25rem 0 0; font: 400 1rem/1.7 system-ui, sans-serif; color: rgba(243,247,251,0.82); }",
		"  </style>",
		"</head>",
		"<body>",
		"  <main>",
		"    <p class=\"eyebrow\">Maintenance mode</p>",
		fmt.Sprintf("    <h1>%s</h1>", html.EscapeString(title)),
		fmt.Sprintf("    <p class=\"copy\">%s</p>", html.EscapeString(body)),
		"  </main>",
		"</body>",
		"</html>",
		"",
	}, "\n")
}

func maintenanceStatePath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "maintenance.json"))
}

func maintenanceRootPath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "maintenance"))
}

func maintenanceIndexPath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(maintenanceRootPath(opts), "index.html"))
}

func renderProxyReloadCommands(opts RemoteOptions, rootCaddyPath, proxySitesDir, proxyDataPath, proxyConfigPath string) []string {
	return []string{
		fmt.Sprintf("if docker ps --format '{{.Names}}' | grep -Fx -- %s >/dev/null 2>&1; then", shellQuote(opts.ProxyContainerName)),
		fmt.Sprintf("  docker exec %s caddy reload --config /etc/caddy/Caddyfile >/dev/null", shellQuote(opts.ProxyContainerName)),
		"elif CADDY_CONTAINER=$(docker ps --filter 'ancestor=caddy:2' --format '{{.Names}}' | head -1) && [ -n \"$CADDY_CONTAINER\" ]; then",
		"  docker exec \"$CADDY_CONTAINER\" caddy reload --config /etc/caddy/Caddyfile >/dev/null 2>&1 || true",
		"else",
		"  docker pull caddy:2 >/dev/null",
		fmt.Sprintf("  docker rm -f %s >/dev/null 2>&1 || true", shellQuote(opts.ProxyContainerName)),
		fmt.Sprintf("  mkdir -p %s", shellQuote("/var/log/caddy")),
		fmt.Sprintf("  docker run -d --restart unless-stopped --network host --name %s -v %s:/etc/caddy/Caddyfile:ro -v %s:/etc/caddy/sites:ro -v %s:/data -v %s:/config -v %s:/var/log/caddy caddy:2 >/dev/null",
			shellQuote(opts.ProxyContainerName),
			shellQuote(rootCaddyPath),
			shellQuote(proxySitesDir),
			shellQuote(proxyDataPath),
			shellQuote(proxyConfigPath),
			shellQuote("/var/log/caddy")),
		"fi",
	}
}
