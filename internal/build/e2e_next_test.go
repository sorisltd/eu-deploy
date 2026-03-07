package build

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorisltd/eu-deploy/internal/config"
	"github.com/sorisltd/eu-deploy/internal/detect"
)

func TestNextStandaloneE2E(t *testing.T) {
	if os.Getenv("EUDEPLOY_E2E") != "1" {
		t.Skip("set EUDEPLOY_E2E=1 to run the live Next.js smoke test")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skipf("npm not available: %v", err)
	}

	fixtureDir := filepath.Join("testdata", "next-standalone-app")
	workDir := t.TempDir()

	if err := copyTree(fixtureDir, workDir); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	mustRunE2ECommand(t, workDir, "npm", "ci", "--no-audit", "--no-fund")

	d := detect.Detect(workDir)
	if d.Framework != "nextjs" {
		t.Fatalf("unexpected framework: %q", d.Framework)
	}
	if d.BuildCommand != "npm run build" {
		t.Fatalf("unexpected build command: %q", d.BuildCommand)
	}
	if d.OutputDir != ".next/standalone" {
		t.Fatalf("unexpected output dir: %q", d.OutputDir)
	}
	if d.StartCommand != "node .next/standalone/server.js" {
		t.Fatalf("unexpected start command: %q", d.StartCommand)
	}

	cfg := config.Default()
	cfg.Project = config.ProjectSpec{
		Name:      d.ProjectName,
		Framework: d.Framework,
	}
	cfg.Build = config.BuildSpec{
		Command: d.BuildCommand,
		Output:  d.OutputDir,
	}
	cfg.Runtime = config.RuntimeSpec{
		Type:  "web",
		Start: d.StartCommand,
		Port:  3000,
	}

	needsInstall, err := RequiresDependencyInstall(cfg, workDir)
	if err != nil {
		t.Fatalf("RequiresDependencyInstall: %v", err)
	}
	if needsInstall {
		t.Fatalf("next standalone should not require dependency installation")
	}

	res, err := BuildProject(cfg, workDir)
	if err != nil {
		t.Fatalf("BuildProject: %v", err)
	}

	artifactPath := filepath.Join(workDir, res.ArtifactPath)
	entries := readTarEntries(t, artifactPath)

	if !containsEntry(entries, ".next/standalone/server.js") {
		t.Fatalf("artifact missing server.js; entries=%v", entries)
	}
	if !containsEntry(entries, ".next/standalone/public/robots.txt") {
		t.Fatalf("artifact missing public asset; entries=%v", entries)
	}
	if !containsEntryWithPrefix(entries, ".next/standalone/.next/static/") {
		t.Fatalf("artifact missing .next/static assets; entries=%v", entries)
	}
}

func mustRunE2ECommand(t *testing.T, dir string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CI=1", "NEXT_TELEMETRY_DISABLED=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
	}
}

func readTarEntries(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	var entries []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read tar: %v", err)
		}
		entries = append(entries, hdr.Name)
	}

	return entries
}

func containsEntry(entries []string, want string) bool {
	for _, entry := range entries {
		if entry == want {
			return true
		}
	}
	return false
}

func containsEntryWithPrefix(entries []string, prefix string) bool {
	for _, entry := range entries {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}
