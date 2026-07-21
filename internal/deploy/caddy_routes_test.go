package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderManagedCaddyfileGolden(t *testing.T) {
	tests := []struct {
		name   string
		golden string
		routes []CaddyRoute
	}{
		{
			name:   "legacy handle_path route",
			golden: "legacy-route.golden",
			routes: []CaddyRoute{{Hostnames: []string{"legacy.example.com"}, Path: "/book", Upstream: "127.0.0.1:3002"}},
		},
		{
			name:   "prefix preserving route",
			golden: "preserve-prefix-route.golden",
			routes: []CaddyRoute{{Hostnames: []string{"module.example.com"}, Path: "/darbai", Upstream: "127.0.0.1:3008", PreservePrefix: true}},
		},
		{
			name:   "shared hostname and redirect",
			golden: "shared-host-redirect.golden",
			routes: []CaddyRoute{
				{Hostnames: []string{"bustora.lt"}, Path: "/", Upstream: "127.0.0.1:3010"},
				{Hostnames: []string{"bustora.lt"}, Path: "/darbai", Upstream: "127.0.0.1:3008", PreservePrefix: true},
				{Hostnames: []string{"www.safebuild.lt", "safebuild.lt"}, Redirect: "https://bustora.lt/meistrams", Code: 301},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "caddy", test.golden))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got := RenderManagedCaddyfile(test.routes); got != string(want) {
				t.Fatalf("rendered Caddyfile mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
	}
}

func TestManagedRouteFragmentsComposeAcrossApps(t *testing.T) {
	bustora := renderManagedRouteConfigCommands(RemoteOptions{
		ProjectName:      "bustora",
		RemoteServerPath: "/opt/eu-deploy",
		RemoteAppPath:    "/opt/eu-deploy/apps/bustora",
		Routes:           []RemoteRoute{{Hostnames: []string{"bustora.lt"}, Path: "/"}},
	}, "127.0.0.1:${TARGET_PORT}")
	safebuild := renderManagedRouteConfigCommands(RemoteOptions{
		ProjectName:      "safebuild",
		RemoteServerPath: "/opt/eu-deploy",
		RemoteAppPath:    "/opt/eu-deploy/apps/safebuild",
		Routes:           []RemoteRoute{{Hostnames: []string{"bustora.lt"}, Path: "/darbai", PreservePrefix: true}},
	}, "127.0.0.1:${TARGET_PORT}")

	bustoraScript := strings.Join(bustora, "\n")
	safebuildScript := strings.Join(safebuild, "\n")
	for _, script := range []string{bustoraScript, safebuildScript} {
		if !strings.Contains(script, "/opt/eu-deploy/_proxy/routes/bustora.lt") {
			t.Fatalf("shared hostname should use the same route registry:\n%s", script)
		}
	}
	if !strings.Contains(bustoraScript, "999999-bustora-000.caddy") {
		t.Fatalf("root fragment must sort last:\n%s", bustoraScript)
	}
	if !strings.Contains(safebuildScript, "999992-safebuild-000.caddy") {
		t.Fatalf("/darbai fragment must sort before root:\n%s", safebuildScript)
	}
}

func TestManagedRouteCommandsComposeSequentialDeploys(t *testing.T) {
	serverRoot := t.TempDir()
	bustoraApp := filepath.Join(serverRoot, "apps", "bustora")
	safebuildApp := filepath.Join(serverRoot, "apps", "safebuild")
	for _, path := range []string{bustoraApp, safebuildApp, filepath.Join(serverRoot, "_proxy", "sites")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	run := func(t *testing.T, port string, opts RemoteOptions) {
		t.Helper()
		script := strings.Join(renderManagedRouteConfigCommands(opts, "127.0.0.1:${TARGET_PORT}"), "\n")
		command := exec.Command("bash", "-c", "set -euo pipefail\n"+script)
		command.Env = append(os.Environ(), "TARGET_PORT="+port)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("route script failed: %v\n%s\n--- script ---\n%s", err, output, script)
		}
	}

	run(t, "3010", RemoteOptions{
		ProjectName:      "bustora",
		AnalyticsLogName: "bustora",
		RemoteServerPath: serverRoot,
		RemoteAppPath:    bustoraApp,
		Routes:           []RemoteRoute{{Hostnames: []string{"bustora.lt"}, Path: "/"}},
	})
	run(t, "3008", RemoteOptions{
		ProjectName:      "safebuild",
		AnalyticsLogName: "safebuild",
		RemoteServerPath: serverRoot,
		RemoteAppPath:    safebuildApp,
		Routes:           []RemoteRoute{{Hostnames: []string{"bustora.lt"}, Path: "/darbai", PreservePrefix: true, CaddyExtra: "header X-Literal $VALUE"}},
	})

	routeDir := filepath.Join(serverRoot, "_proxy", "routes", "bustora.lt")
	entries, err := os.ReadDir(routeDir)
	if err != nil {
		t.Fatalf("read route registry: %v", err)
	}
	var caddyFiles []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".caddy") {
			caddyFiles = append(caddyFiles, entry.Name())
		}
	}
	if strings.Join(caddyFiles, ",") != "999992-safebuild-000.caddy,999999-bustora-000.caddy" {
		t.Fatalf("unexpected route order: %v", caddyFiles)
	}
	moduleFragment, err := os.ReadFile(filepath.Join(routeDir, caddyFiles[0]))
	if err != nil {
		t.Fatalf("read module fragment: %v", err)
	}
	for _, expected := range []string{"header X-Literal $VALUE", "handle /darbai*", "reverse_proxy 127.0.0.1:3008"} {
		if !strings.Contains(string(moduleFragment), expected) {
			t.Fatalf("module fragment missing %q:\n%s", expected, moduleFragment)
		}
	}
	site, err := os.ReadFile(filepath.Join(serverRoot, "_proxy", "sites", "bustora.lt.caddy"))
	if err != nil {
		t.Fatalf("read composed site: %v", err)
	}
	if !strings.Contains(string(site), "output file /var/log/caddy/bustora.access.log") {
		t.Fatalf("root route should own shared access log:\n%s", site)
	}
}

func TestManagedRouteDeployRebuildsSharedHostFromCurrentMaintenanceState(t *testing.T) {
	serverRoot := t.TempDir()
	bustoraApp := filepath.Join(serverRoot, "apps", "bustora")
	safebuildApp := filepath.Join(serverRoot, "apps", "safebuild")
	for _, path := range []string{bustoraApp, safebuildApp, filepath.Join(serverRoot, "_proxy", "sites")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	bustora := RemoteOptions{
		ProjectName:      "bustora",
		AnalyticsLogName: "bustora",
		RemoteServerPath: serverRoot,
		RemoteAppPath:    bustoraApp,
		Routes:           []RemoteRoute{{Hostnames: []string{"bustora.lt"}, Path: "/"}},
	}
	safebuild := RemoteOptions{
		ProjectName:      "safebuild",
		AnalyticsLogName: "safebuild",
		RemoteServerPath: serverRoot,
		RemoteAppPath:    safebuildApp,
		Routes:           []RemoteRoute{{Hostnames: []string{"bustora.lt"}, Path: "/darbai", PreservePrefix: true}},
	}
	run := func(port string, opts RemoteOptions) {
		t.Helper()
		script := strings.Join(renderManagedRouteConfigCommands(opts, "127.0.0.1:${TARGET_PORT}"), "\n")
		command := exec.Command("bash", "-c", "set -euo pipefail\n"+script)
		command.Env = append(os.Environ(), "TARGET_PORT="+port)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("route script failed: %v\n%s\n--- script ---\n%s", err, output, script)
		}
	}
	read := func(path string) string {
		t.Helper()
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(contents)
	}

	run("3010", bustora)
	run("3008", safebuild)
	if err := os.WriteFile(filepath.Join(safebuildApp, "maintenance.json"), []byte(`{"enabled":true}`), 0o644); err != nil {
		t.Fatalf("write maintenance state: %v", err)
	}
	run("3009", safebuild)

	routeDir := filepath.Join(serverRoot, "_proxy", "routes", "bustora.lt")
	sitePath := filepath.Join(serverRoot, "_proxy", "sites", "bustora.lt.caddy")
	bustoraFragment := read(filepath.Join(routeDir, "999999-bustora-000.caddy"))
	safebuildFragment := read(filepath.Join(routeDir, "999992-safebuild-000.caddy"))
	site := read(sitePath)
	if !strings.Contains(bustoraFragment, "reverse_proxy 127.0.0.1:3010") {
		t.Fatalf("deploying maintained app dropped the sibling fragment:\n%s", bustoraFragment)
	}
	if !strings.Contains(safebuildFragment, "root * "+filepath.Join(safebuildApp, "maintenance")) || strings.Contains(safebuildFragment, "reverse_proxy") {
		t.Fatalf("maintained app did not get a route-scoped maintenance fragment:\n%s", safebuildFragment)
	}
	if !strings.Contains(site, "import "+filepath.Join(serverRoot, "_proxy", "routes", "bustora.lt")+"/*.caddy") {
		t.Fatalf("shared site was not freshly composed from the route registry:\n%s", site)
	}

	if err := os.Remove(filepath.Join(safebuildApp, "maintenance.json")); err != nil {
		t.Fatalf("remove maintenance state: %v", err)
	}
	run("3009", safebuild)
	safebuildFragment = read(filepath.Join(routeDir, "999992-safebuild-000.caddy"))
	if !strings.Contains(safebuildFragment, "reverse_proxy 127.0.0.1:3009") || strings.Contains(safebuildFragment, "root * ") {
		t.Fatalf("disabling maintenance restored stale route state:\n%s", safebuildFragment)
	}
	if !strings.Contains(read(filepath.Join(routeDir, "999999-bustora-000.caddy")), "reverse_proxy 127.0.0.1:3010") {
		t.Fatal("disabling maintenance dropped the sibling app route")
	}
}
