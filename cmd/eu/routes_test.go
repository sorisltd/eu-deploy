package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sorisltd/eu-deploy/internal/config"
	"github.com/sorisltd/eu-deploy/internal/deploy"
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

func TestApplyEdgeOptionsSkipsRedirectsAndNil(t *testing.T) {
	routes := []deploy.RemoteRoute{
		{Hostnames: []string{"bustora.lt"}, Path: "/"},
		{Hostnames: []string{"safebuild.lt"}, Redirect: "https://bustora.lt/meistrams", Code: 301},
	}
	applyEdgeOptions(routes, nil)
	if routes[0].Edge.Enabled() || routes[1].Edge.Enabled() {
		t.Fatalf("nil edge must leave routes untouched: %#v", routes)
	}
	applyEdgeOptions(routes, &config.EdgeSpec{SecurityHeaders: true, MaxBodySize: " 10MB "})
	if !routes[0].Edge.SecurityHeaders || routes[0].Edge.MaxBodySize != "10MB" {
		t.Fatalf("proxy route should get edge options: %#v", routes[0].Edge)
	}
	if routes[1].Edge.Enabled() {
		t.Fatalf("redirect route must not get edge options: %#v", routes[1].Edge)
	}
}
