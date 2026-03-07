package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sorisltd/eu-deploy/internal/config"
)

func TestResolvePackageSourceStagesNextStandaloneAssets(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Config{
		Project: config.ProjectSpec{
			Framework: "nextjs",
		},
		Build: config.BuildSpec{
			Output: ".next/standalone",
		},
	}

	mustWritePackageFile(t, filepath.Join(workDir, ".next", "standalone", "server.js"), "console.log('server')\n")
	mustWritePackageFile(t, filepath.Join(workDir, ".next", "standalone", "package.json"), "{\"name\":\"next\"}\n")
	mustWritePackageFile(t, filepath.Join(workDir, ".next", "static", "chunks", "app.js"), "chunk\n")
	mustWritePackageFile(t, filepath.Join(workDir, "public", "favicon.ico"), "ico\n")

	pkgSource, err := ResolvePackageSource(cfg, workDir)
	if err != nil {
		t.Fatalf("ResolvePackageSource: %v", err)
	}
	defer func() { _ = pkgSource.Cleanup() }()

	if pkgSource.ArchiveRoot != "" {
		t.Fatalf("expected rootless archive for next standalone, got %q", pkgSource.ArchiveRoot)
	}
	if pkgSource.RequiresDependencyInstall {
		t.Fatalf("next standalone should not require dependency installation")
	}

	assertFileExists(t, filepath.Join(pkgSource.SourceDir, ".next", "standalone", "server.js"))
	assertFileExists(t, filepath.Join(pkgSource.SourceDir, ".next", "standalone", ".next", "static", "chunks", "app.js"))
	assertFileExists(t, filepath.Join(pkgSource.SourceDir, ".next", "standalone", "public", "favicon.ico"))
}

func TestResolvePackageSourceStagesNextStandardAssets(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Config{
		Project: config.ProjectSpec{
			Framework: "nextjs",
		},
		Build: config.BuildSpec{
			Output: ".next",
		},
	}

	mustWritePackageFile(t, filepath.Join(workDir, ".next", "BUILD_ID"), "build-id\n")
	mustWritePackageFile(t, filepath.Join(workDir, ".next", "static", "chunks", "app.js"), "chunk\n")
	mustWritePackageFile(t, filepath.Join(workDir, "public", "favicon.ico"), "ico\n")

	pkgSource, err := ResolvePackageSource(cfg, workDir)
	if err != nil {
		t.Fatalf("ResolvePackageSource: %v", err)
	}
	defer func() { _ = pkgSource.Cleanup() }()

	if pkgSource.ArchiveRoot != "" {
		t.Fatalf("expected rootless archive for next standard build, got %q", pkgSource.ArchiveRoot)
	}
	if !pkgSource.RequiresDependencyInstall {
		t.Fatalf("next standard build should require dependency installation")
	}

	assertFileExists(t, filepath.Join(pkgSource.SourceDir, ".next", "BUILD_ID"))
	assertFileExists(t, filepath.Join(pkgSource.SourceDir, "public", "favicon.ico"))
}

func TestRequiresDependencyInstallForNextStandalone(t *testing.T) {
	cfg := config.Config{
		Project: config.ProjectSpec{Framework: "nextjs"},
		Build:   config.BuildSpec{Output: ".next/standalone"},
	}

	needsInstall, err := RequiresDependencyInstall(cfg, t.TempDir())
	if err != nil {
		t.Fatalf("RequiresDependencyInstall: %v", err)
	}
	if needsInstall {
		t.Fatalf("next standalone should not require dependency installation")
	}
}

func mustWritePackageFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}
