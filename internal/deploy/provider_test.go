package deploy

import (
	"strings"
	"testing"

	"github.com/sorisltd/eu-deploy/internal/config"
)

func TestPreferredDeployTargetUsesDeployProvider(t *testing.T) {
	cfg := config.Config{
		Deploy: config.DeploySpec{Provider: "ovh"},
	}

	if got := PreferredDeployTarget(cfg, "docker"); got != "ovh" {
		t.Fatalf("expected deploy.provider to win, got %q", got)
	}
}

func TestPreferredDeployTargetInfersSingleRemoteProvider(t *testing.T) {
	cfg := config.Config{
		Scaleway: &config.ScalewaySpec{},
	}

	if got := PreferredDeployTarget(cfg, "docker"); got != "scaleway" {
		t.Fatalf("expected single provider inference, got %q", got)
	}
}

func TestRenderDestroyRemoteScriptDropsDatabase(t *testing.T) {
	got := renderDestroyRemoteScript(RemoteOptions{
		RemoteAppPath:      "/opt/eu-deploy/apps/massage",
		RemoteServerPath:   "/opt/eu-deploy",
		ProxyContainerName: sharedProxyContainer,
		AppContainerName:   "eu-massage-app",
		ImageTag:           "eu-deploy-massage:remote",
		SiteConfigName:     "massage.example.com.caddy",
		SharedDatabase: &SharedDatabaseOptions{
			Name: "massage",
			User: "massage",
		},
	}, true)

	for _, expected := range []string{
		`docker rm -f 'eu-massage-app-a'`,
		`DROP DATABASE IF EXISTS "massage";`,
		`DROP ROLE IF EXISTS "massage";`,
		`docker exec 'eu-shared-caddy' caddy reload --config /etc/caddy/Caddyfile`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("destroy script missing %q:\n%s", expected, got)
		}
	}
}

func TestRenderRollbackRemoteScriptUsesTargetImageAndReloadsProxy(t *testing.T) {
	got := renderRollbackRemoteScript(RemoteOptions{
		RemoteServerPath:   "/opt/eu-deploy",
		RemoteAppPath:      "/opt/eu-deploy/apps/massage",
		ProxyContainerName: sharedProxyContainer,
		AppContainerName:   "eu-massage-app",
		Hostname:           "massage.example.com",
		RoutePath:          "/",
		HealthcheckPath:    "/",
		ServicePort:        3001,
		ContainerPort:      3000,
		ImageTag:           "eu-deploy-massage:remote",
		SiteConfigName:     "massage.example.com.caddy",
	}, ReleaseRecord{
		ID:          "release-123",
		Image:       "eu-deploy-massage:release-123",
		ArtifactSHA: "abc123",
	})

	for _, expected := range []string{
		`TARGET_IMAGE='eu-deploy-massage:release-123'`,
		`docker exec 'eu-shared-caddy' caddy reload --config /etc/caddy/Caddyfile`,
		`printf '%s\n' "$TARGET_RELEASE" > "$CURRENT_RELEASE_FILE"`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("rollback script missing %q:\n%s", expected, got)
		}
	}
}
