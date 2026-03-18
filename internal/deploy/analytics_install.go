package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const analyticsWorkerBinaryName = "eu-analytics-worker"

func InstallRemoteAnalytics(opts RemoteOptions, localBinaryPath string) error {
	if strings.TrimSpace(opts.RemoteHost) == "" {
		return fmt.Errorf("%s.host is required", providerConfigFieldPrefix(opts.Provider))
	}
	if strings.TrimSpace(opts.RemoteUser) == "" {
		return fmt.Errorf("%s.user is required", providerConfigFieldPrefix(opts.Provider))
	}
	if opts.RemotePort <= 0 {
		return fmt.Errorf("%s.port is required", providerConfigFieldPrefix(opts.Provider))
	}
	if strings.TrimSpace(opts.RemoteServerPath) == "" {
		return fmt.Errorf("%s.server_path is required", providerConfigFieldPrefix(opts.Provider))
	}
	if strings.TrimSpace(localBinaryPath) == "" {
		return fmt.Errorf("local analytics worker path is required")
	}

	tempName := fmt.Sprintf("%s.upload", analyticsWorkerBinaryName)
	remoteUploadPath := filepath.ToSlash(filepath.Join(opts.RemoteServerPath, tempName))

	if err := uploadAnalyticsBinary(opts, localBinaryPath, remoteUploadPath); err != nil {
		return err
	}

	script := renderAnalyticsInstallScript(opts, remoteUploadPath)
	return runRemoteScript(opts, script)
}

func uploadAnalyticsBinary(opts RemoteOptions, localBinaryPath, remoteUploadPath string) error {
	args := buildSCPArgs(opts)
	args = append(args, localBinaryPath, fmt.Sprintf("%s@%s:%s", opts.RemoteUser, opts.RemoteHost, remoteUploadPath))

	cmd := exec.Command("scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func renderAnalyticsInstallScript(opts RemoteOptions, remoteUploadPath string) string {
	serverRoot := strings.TrimSpace(opts.RemoteServerPath)
	analyticsRoot := filepath.ToSlash(filepath.Join(serverRoot, "analytics"))
	analyticsBinDir := filepath.ToSlash(filepath.Join(analyticsRoot, "bin"))
	analyticsLogsDir := filepath.ToSlash(filepath.Join(analyticsRoot, "logs"))
	analyticsMaxmindDir := filepath.ToSlash(filepath.Join(analyticsRoot, "maxmind"))
	analyticsSecret := filepath.ToSlash(filepath.Join(analyticsRoot, "analytics.secret"))
	scriptsDir := filepath.ToSlash(filepath.Join(serverRoot, "scripts"))
	processScriptPath := filepath.ToSlash(filepath.Join(scriptsDir, "analytics-process.sh"))
	aggregateScriptPath := filepath.ToSlash(filepath.Join(scriptsDir, "analytics-aggregate.sh"))
	refreshScriptPath := filepath.ToSlash(filepath.Join(scriptsDir, "analytics-refresh-maxmind.sh"))
	workerPath := filepath.ToSlash(filepath.Join(analyticsBinDir, analyticsWorkerBinaryName))
	maxmindEnvPath := filepath.ToSlash(filepath.Join(analyticsMaxmindDir, "maxmind.env"))
	processLog := filepath.ToSlash(filepath.Join(analyticsLogsDir, "process.log"))
	aggregateLog := filepath.ToSlash(filepath.Join(analyticsLogsDir, "aggregate.log"))
	refreshLog := filepath.ToSlash(filepath.Join(analyticsLogsDir, "maxmind.log"))
	processCron := fmt.Sprintf("*/5 * * * * %s >> %s 2>&1", processScriptPath, processLog)
	aggregateCron := fmt.Sprintf("5 0 * * * %s >> %s 2>&1", aggregateScriptPath, aggregateLog)
	refreshCron := fmt.Sprintf("17 3 * * 1 %s >> %s 2>&1", refreshScriptPath, refreshLog)

	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("mkdir -p %s %s %s %s %s %s",
			shellQuote(serverRoot),
			shellQuote(analyticsBinDir),
			shellQuote(analyticsLogsDir),
			shellQuote(analyticsMaxmindDir),
			shellQuote(scriptsDir),
			shellQuote("/var/log/caddy")),
		fmt.Sprintf("install -m 755 %s %s", shellQuote(remoteUploadPath), shellQuote(workerPath)),
		fmt.Sprintf("rm -f %s", shellQuote(remoteUploadPath)),
		fmt.Sprintf("if [ ! -f %s ]; then", shellQuote(analyticsSecret)),
		fmt.Sprintf("  od -An -tx1 -N32 /dev/urandom | tr -d ' \\n' > %s", shellQuote(analyticsSecret)),
		fmt.Sprintf("  chmod 600 %s", shellQuote(analyticsSecret)),
		"fi",
		fmt.Sprintf("if [ ! -f %s ]; then", shellQuote(maxmindEnvPath)),
		fmt.Sprintf("  cat > %s <<'EOF'\nMAXMIND_ACCOUNT_ID=\nMAXMIND_LICENSE_KEY=\nEOF", shellQuote(maxmindEnvPath)),
		fmt.Sprintf("  chmod 600 %s", shellQuote(maxmindEnvPath)),
		"fi",
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(processScriptPath), renderAnalyticsProcessWrapper(serverRoot, workerPath, analyticsSecret)),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(aggregateScriptPath), renderAnalyticsAggregateWrapper(serverRoot, workerPath)),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(refreshScriptPath), renderAnalyticsRefreshScript(analyticsMaxmindDir, maxmindEnvPath)),
		fmt.Sprintf("chmod 755 %s %s %s", shellQuote(processScriptPath), shellQuote(aggregateScriptPath), shellQuote(refreshScriptPath)),
		fmt.Sprintf("%s init-schema --server-root %s", shellQuote(workerPath), shellQuote(serverRoot)),
		"CURRENT_CRONTAB=\"$(crontab -l 2>/dev/null || true)\"",
		"{",
		"  if [ -n \"$CURRENT_CRONTAB\" ]; then",
		fmt.Sprintf("    printf '%%s\\n' \"$CURRENT_CRONTAB\" | grep -Fv %s | grep -Fv %s | grep -Fv %s || true",
			shellQuote(processScriptPath),
			shellQuote(aggregateScriptPath),
			shellQuote(refreshScriptPath)),
		"  fi",
		fmt.Sprintf("  printf '%%s\\n' %s %s %s",
			shellQuote(processCron),
			shellQuote(aggregateCron),
			shellQuote(refreshCron)),
		"} | crontab -",
		fmt.Sprintf("%s || true", shellQuote(processScriptPath)),
	}

	return strings.Join(lines, "\n")
}

func renderAnalyticsProcessWrapper(serverRoot, workerPath, secretPath string) string {
	return strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		fmt.Sprintf("exec %s process --server-root %s --secret-file %s --logs-dir %s",
			shellQuote(workerPath),
			shellQuote(serverRoot),
			shellQuote(secretPath),
			shellQuote("/var/log/caddy")),
		"",
	}, "\n")
}

func renderAnalyticsAggregateWrapper(serverRoot, workerPath string) string {
	return strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		fmt.Sprintf("exec %s aggregate --server-root %s",
			shellQuote(workerPath),
			shellQuote(serverRoot)),
		"",
	}, "\n")
}

func renderAnalyticsRefreshScript(maxmindDir, envPath string) string {
	return strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		fmt.Sprintf(". %s", shellQuote(envPath)),
		"if [ -z \"${MAXMIND_ACCOUNT_ID:-}\" ] || [ -z \"${MAXMIND_LICENSE_KEY:-}\" ]; then",
		"  echo 'MAXMIND_ACCOUNT_ID and MAXMIND_LICENSE_KEY are required in maxmind.env; skipping refresh.'",
		"  exit 0",
		"fi",
		"tmp_dir=\"$(mktemp -d)\"",
		"cleanup() { rm -rf \"$tmp_dir\"; }",
		"trap cleanup EXIT",
		fmt.Sprintf("mkdir -p %s", shellQuote(maxmindDir)),
		"for edition in GeoLite2-City GeoLite2-ASN; do",
		"  archive=\"$tmp_dir/${edition}.tar.gz\"",
		"  curl -fsSL -u \"$MAXMIND_ACCOUNT_ID:$MAXMIND_LICENSE_KEY\" \"https://download.maxmind.com/geoip/databases/${edition}/download?suffix=tar.gz\" -o \"$archive\"",
		"  tar -xzf \"$archive\" -C \"$tmp_dir\"",
		"  mmdb_path=\"$(find \"$tmp_dir\" -maxdepth 2 -type f -name \"${edition}.mmdb\" | head -n 1)\"",
		"  if [ -z \"$mmdb_path\" ]; then",
		"    echo \"Failed to locate ${edition}.mmdb after download.\" >&2",
		"    exit 1",
		"  fi",
		fmt.Sprintf("  install -m 644 \"$mmdb_path\" %s/${edition}.mmdb", shellQuote(maxmindDir)),
		"done",
		"",
	}, "\n")
}
