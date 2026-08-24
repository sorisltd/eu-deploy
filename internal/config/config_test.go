package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRoutesRedirectDefaultsAndHTTPS(t *testing.T) {
	valid := []RouteSpec{{Hostnames: []string{"safebuild.lt", "www.safebuild.lt"}, Redirect: "https://bustora.lt/meistrams"}}
	if err := ValidateRoutes(valid); err != nil {
		t.Fatalf("valid redirect rejected: %v", err)
	}

	invalid := []RouteSpec{{Hostname: "safebuild.lt", Redirect: "http://bustora.lt/meistrams"}}
	if err := ValidateRoutes(invalid); err == nil {
		t.Fatal("expected non-https redirect to fail")
	}
}

func TestValidateRoutesLegacyAndPreservePrefix(t *testing.T) {
	routes := []RouteSpec{
		{Hostname: "legacy.example.com", Path: "/book", Target: "web"},
		{Hostname: "bustora.lt", Path: "/darbai", Target: "web", PreservePrefix: true},
	}
	if err := ValidateRoutes(routes); err != nil {
		t.Fatalf("proxy routes rejected: %v", err)
	}
}

func TestRuntimeImageResolvesNodeVersionFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".node-version"), []byte("22.22.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Runtime: RuntimeSpec{
		Image:           "node:${NODE_VERSION}-bookworm-slim@sha256:abc",
		NodeVersionFile: ".node-version",
	}}
	got, err := RuntimeImage(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "node:22.22.0-bookworm-slim@sha256:abc"
	if got != want {
		t.Fatalf("runtime image mismatch: got %q want %q", got, want)
	}
}

func TestRuntimeImageRequiresExactNodeVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".node-version"), []byte("22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Runtime: RuntimeSpec{
		Image:           "node:${NODE_VERSION}-bookworm-slim@sha256:abc",
		NodeVersionFile: ".node-version",
	}}
	if _, err := RuntimeImage(cfg, dir); err == nil {
		t.Fatal("expected an inexact Node version to fail")
	}
}
