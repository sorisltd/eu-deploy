package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sorisltd/eu-deploy/internal/config"
)

func TestBuildRemoteRoutesLoadsExtraAndDefaultsRedirect(t *testing.T) {
	dir := t.TempDir()
	extraPath := filepath.Join(dir, "assets.caddy")
	if err := os.WriteFile(extraPath, []byte("handle_path /darbai/assets/* {\n  respond 204\n}\n"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}

	routes, err := buildRemoteRoutes(dir, []config.RouteSpec{
		{Hostname: "bustora.lt", Path: "/darbai", Target: "web", PreservePrefix: true, CaddyExtraFile: "assets.caddy"},
		{Hostnames: []string{"safebuild.lt", "www.safebuild.lt"}, Redirect: "https://bustora.lt/meistrams"},
	})
	if err != nil {
		t.Fatalf("buildRemoteRoutes: %v", err)
	}
	if len(routes) != 2 || routes[0].CaddyExtra == "" || !routes[0].PreservePrefix {
		t.Fatalf("unexpected proxy route: %#v", routes)
	}
	if routes[1].Code != 301 || len(routes[1].Hostnames) != 2 {
		t.Fatalf("unexpected redirect route: %#v", routes[1])
	}
}
