package deploy

import (
	"strings"
	"testing"
)

func TestRenderAnalyticsInstallScriptHandlesEmptyCrontab(t *testing.T) {
	script := renderAnalyticsInstallScript(RemoteOptions{
		Provider:         RemoteTargetHetzner,
		RemoteHost:       "89.167.126.100",
		RemoteUser:       "root",
		RemotePort:       22,
		RemoteServerPath: "/opt/eu-deploy",
	}, "/opt/eu-deploy/eu-analytics-worker.upload")

	if !strings.Contains(script, `CURRENT_CRONTAB="$(crontab -l 2>/dev/null || true)"`) {
		t.Fatalf("expected install script to tolerate an empty crontab, got:\n%s", script)
	}

	if !strings.Contains(script, "} | crontab -") {
		t.Fatalf("expected install script to rebuild crontab contents, got:\n%s", script)
	}
}
