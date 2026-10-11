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

func TestValidateEdge(t *testing.T) {
	valid := []*EdgeSpec{
		nil,
		{},
		{SecurityHeaders: true, CacheStatic: true, BlockProbes: true},
		{MaxBodySize: "10MB"},
		{MaxBodySize: "512KB"},
		{MaxBodySize: "1GB"},
	}
	for _, edge := range valid {
		if err := ValidateEdge(edge); err != nil {
			t.Fatalf("ValidateEdge(%#v) = %v, want nil", edge, err)
		}
	}
	invalid := []string{"10", "10mb", "0MB", "10 MB", "1.5MB", "10TB", "-1MB"}
	for _, size := range invalid {
		if err := ValidateEdge(&EdgeSpec{MaxBodySize: size}); err == nil {
			t.Fatalf("ValidateEdge(max_body_size=%q) = nil, want error", size)
		}
	}
}

func TestReadYAMLWithoutEdgeLeavesEdgeNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eudeploy.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nproject:\n  name: demo\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := ReadYAML(path)
	if err != nil {
		t.Fatalf("ReadYAML: %v", err)
	}
	if cfg.Edge != nil {
		t.Fatalf("absent edge section must stay nil, got %#v", cfg.Edge)
	}
}

func TestReadYAMLParsesEdge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eudeploy.yaml")
	body := "version: 1\nedge:\n  security_headers: true\n  cache_static: true\n  block_probes: true\n  max_body_size: 25MB\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := ReadYAML(path)
	if err != nil {
		t.Fatalf("ReadYAML: %v", err)
	}
	want := EdgeSpec{SecurityHeaders: true, CacheStatic: true, BlockProbes: true, MaxBodySize: "25MB"}
	if cfg.Edge == nil || *cfg.Edge != want {
		t.Fatalf("edge = %#v, want %#v", cfg.Edge, want)
	}
}
