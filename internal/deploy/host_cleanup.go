package deploy

import (
	"fmt"
	"strings"
)

const (
	defaultHostCleanupImageMaxAge   = "168h"
	defaultHostCleanupBuilderMaxAge = "168h"
	defaultHostCleanupJournalAge    = "14d"
	defaultHostCleanupJournalSize   = "512M"
)

func renderHostCleanupScript() string {
	lines := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"",
		"IMAGE_MAX_AGE=\"${EUDEPLOY_DOCKER_IMAGE_MAX_AGE:-" + defaultHostCleanupImageMaxAge + "}\"",
		"BUILDER_MAX_AGE=\"${EUDEPLOY_DOCKER_BUILDER_MAX_AGE:-" + defaultHostCleanupBuilderMaxAge + "}\"",
		"JOURNAL_MAX_AGE=\"${EUDEPLOY_JOURNAL_MAX_AGE:-" + defaultHostCleanupJournalAge + "}\"",
		"JOURNAL_MAX_SIZE=\"${EUDEPLOY_JOURNAL_MAX_SIZE:-" + defaultHostCleanupJournalSize + "}\"",
		"",
		"if command -v docker >/dev/null 2>&1; then",
		"  docker image prune -af --filter \"until=${IMAGE_MAX_AGE}\" >/dev/null 2>&1 || true",
		"  docker builder prune -af --filter \"until=${BUILDER_MAX_AGE}\" >/dev/null 2>&1 || true",
		"fi",
		"",
		"if command -v journalctl >/dev/null 2>&1; then",
		"  journalctl --vacuum-time=\"${JOURNAL_MAX_AGE}\" >/dev/null 2>&1 || true",
		"  journalctl --vacuum-size=\"${JOURNAL_MAX_SIZE}\" >/dev/null 2>&1 || true",
		"fi",
	}
	return strings.Join(lines, "\n")
}

func renderHostCleanupService() string {
	lines := []string{
		"[Unit]",
		"Description=Prune unused eu-deploy host artifacts",
		"After=docker.service systemd-journald.service",
		"Wants=docker.service",
		"",
		"[Service]",
		"Type=oneshot",
		"EnvironmentFile=-/etc/default/eu-deploy-host-cleanup",
		"ExecStart=/usr/local/bin/eu-deploy-host-cleanup",
	}
	return strings.Join(lines, "\n")
}

func renderHostCleanupTimer() string {
	lines := []string{
		"[Unit]",
		"Description=Run eu-deploy host cleanup twice a day",
		"",
		"[Timer]",
		"OnBootSec=10m",
		"OnUnitActiveSec=12h",
		"AccuracySec=15m",
		"Persistent=true",
		"Unit=eu-deploy-host-cleanup.service",
		"",
		"[Install]",
		"WantedBy=timers.target",
	}
	return strings.Join(lines, "\n")
}

func renderHostCleanupInstallCommands() []string {
	return []string{
		"$SUDO mkdir -p /etc/default",
		"$SUDO mkdir -p /usr/local/bin",
		fmt.Sprintf("cat > %s <<'EOF'\n%s\nEOF", shellQuote("/usr/local/bin/eu-deploy-host-cleanup"), renderHostCleanupScript()),
		fmt.Sprintf("cat > %s <<'EOF'\n%s\nEOF", shellQuote("/etc/systemd/system/eu-deploy-host-cleanup.service"), renderHostCleanupService()),
		fmt.Sprintf("cat > %s <<'EOF'\n%s\nEOF", shellQuote("/etc/systemd/system/eu-deploy-host-cleanup.timer"), renderHostCleanupTimer()),
		fmt.Sprintf("$SUDO chmod 0755 %s", shellQuote("/usr/local/bin/eu-deploy-host-cleanup")),
		"$SUDO systemctl daemon-reload",
		"$SUDO systemctl enable --now eu-deploy-host-cleanup.timer",
		"$SUDO systemctl start eu-deploy-host-cleanup.service || true",
	}
}

func renderHostCleanupRunCommand() string {
	return "if [ -x /usr/local/bin/eu-deploy-host-cleanup ]; then /usr/local/bin/eu-deploy-host-cleanup >/dev/null 2>&1 || true; fi"
}
