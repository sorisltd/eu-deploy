package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sorisltd/eu-deploy/internal/config"
)

func TestComputeInputHashIgnoresGeneratedPaths(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Config{
		Build: config.BuildSpec{
			Output: "dist",
		},
	}

	mustWriteFile(t, filepath.Join(workDir, "src", "index.js"), "console.log('a')\n")
	mustWriteFile(t, filepath.Join(workDir, "dist", "index.html"), "<h1>old</h1>\n")
	mustWriteFile(t, filepath.Join(workDir, "node_modules", "pkg", "index.js"), "module.exports = 1\n")
	mustWriteFile(t, filepath.Join(workDir, ".eudeploy", "build.json"), "{\"artifact_path\":\"x\"}\n")

	hashBefore, err := ComputeInputHash(cfg, workDir)
	if err != nil {
		t.Fatalf("ComputeInputHash before change: %v", err)
	}

	mustWriteFile(t, filepath.Join(workDir, "dist", "index.html"), "<h1>new</h1>\n")
	mustWriteFile(t, filepath.Join(workDir, "node_modules", "pkg", "index.js"), "module.exports = 2\n")
	mustWriteFile(t, filepath.Join(workDir, ".eudeploy", "build.json"), "{\"artifact_path\":\"y\"}\n")

	hashAfterGeneratedChange, err := ComputeInputHash(cfg, workDir)
	if err != nil {
		t.Fatalf("ComputeInputHash after generated change: %v", err)
	}
	if hashBefore != hashAfterGeneratedChange {
		t.Fatalf("generated files should be ignored, got %q != %q", hashBefore, hashAfterGeneratedChange)
	}

	mustWriteFile(t, filepath.Join(workDir, "src", "index.js"), "console.log('b')\n")

	hashAfterSourceChange, err := ComputeInputHash(cfg, workDir)
	if err != nil {
		t.Fatalf("ComputeInputHash after source change: %v", err)
	}
	if hashBefore == hashAfterSourceChange {
		t.Fatalf("source change should affect input hash")
	}
}

func TestEnsureArtifactDetectsStaleInputs(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Config{
		Project: config.ProjectSpec{Name: "demo"},
		Build: config.BuildSpec{
			Command: "mkdir -p dist && printf 'hello' > dist/index.html",
			Output:  "dist",
		},
	}

	mustWriteFile(t, filepath.Join(workDir, "src", "index.js"), "console.log('a')\n")

	res, err := BuildProject(cfg, workDir)
	if err != nil {
		t.Fatalf("BuildProject: %v", err)
	}
	if res.InputHash == "" {
		t.Fatalf("BuildProject should persist an input hash")
	}

	reused, built, err := EnsureArtifact(cfg, workDir, false)
	if err != nil {
		t.Fatalf("EnsureArtifact without changes: %v", err)
	}
	if built {
		t.Fatalf("EnsureArtifact should reuse a fresh artifact")
	}
	if reused.InputHash != res.InputHash {
		t.Fatalf("reused artifact should keep original input hash")
	}

	mustWriteFile(t, filepath.Join(workDir, "src", "index.js"), "console.log('changed')\n")

	_, _, err = EnsureArtifact(cfg, workDir, false)
	if err == nil {
		t.Fatalf("EnsureArtifact should reject stale artifacts")
	}
	if err.Error() != "build artifacts are stale: run `eu build` first" {
		t.Fatalf("unexpected stale error: %v", err)
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
