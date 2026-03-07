package deploy

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeDockerName(t *testing.T) {
	if got := SanitizeDockerName(" My App/Prod "); got != "my-app-prod" {
		t.Fatalf("unexpected sanitized name: %q", got)
	}
	if got := SanitizeDockerName("!!!"); got != "app" {
		t.Fatalf("expected fallback name, got %q", got)
	}
}

func TestDockerfileContentsUsesRelativeArtifactPath(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "demo")
	opts := DockerOptions{
		WorkDir:             workDir,
		ArtifactPath:        filepath.Join(workDir, ".eudeploy", "demo.tar.gz"),
		RuntimeStart:        "node server.js",
		ContainerPort:       3000,
		InstallDependencies: true,
	}

	dockerfile := dockerfileContents(opts)

	if !strings.Contains(dockerfile, "ADD .eudeploy/demo.tar.gz /app/") {
		t.Fatalf("dockerfile should use a workdir-relative artifact path:\n%s", dockerfile)
	}
	if !strings.Contains(dockerfile, "CMD [\"bash\",\"-lc\",\"node server.js\"]") {
		t.Fatalf("dockerfile should quote runtime.start as a JSON string:\n%s", dockerfile)
	}
}

func TestDockerfileContentsSkipsPackageInstallWhenNotNeeded(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "demo")
	opts := DockerOptions{
		WorkDir:             workDir,
		ArtifactPath:        filepath.Join(workDir, ".eudeploy", "demo.tar.gz"),
		RuntimeStart:        "node .next/standalone/server.js",
		ContainerPort:       3000,
		InstallDependencies: false,
	}

	dockerfile := dockerfileContents(opts)

	if strings.Contains(dockerfile, "package*.json") {
		t.Fatalf("dockerfile should not copy package manifests when install is skipped:\n%s", dockerfile)
	}
	if strings.Contains(dockerfile, "npm ci --omit=dev") {
		t.Fatalf("dockerfile should not install dependencies when install is skipped:\n%s", dockerfile)
	}
}
