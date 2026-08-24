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

func TestDockerfileContentsAddsPostDeployArchive(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "demo")
	opts := DockerOptions{
		WorkDir:           workDir,
		ArtifactPath:      filepath.Join(workDir, ".eudeploy", "demo.tar.gz"),
		PostDeployArchive: filepath.Join(workDir, ".eudeploy", "postdeploy.tar.gz"),
		RuntimeStart:      "npm run start",
		ContainerPort:     3000,
	}

	dockerfile := dockerfileContents(opts)

	if !strings.Contains(dockerfile, "ADD .eudeploy/postdeploy.tar.gz /app/") {
		t.Fatalf("dockerfile should add the post-deploy archive when configured:\n%s", dockerfile)
	}
}

func TestDockerfileContentsUsesPinnedBaseImageAndNPMConfig(t *testing.T) {
	opts := DockerOptions{
		WorkDir:             "/tmp/project",
		ArtifactPath:        "/tmp/project/.eudeploy/demo.tar.gz",
		RuntimeStart:        "node server.js",
		ContainerPort:       3000,
		InstallDependencies: true,
		BaseImage:           "node:22.22.0-bookworm-slim@sha256:abc",
		HasNPMConfig:        true,
	}
	dockerfile := dockerfileContents(opts)
	if !strings.Contains(dockerfile, "FROM node:22.22.0-bookworm-slim@sha256:abc") {
		t.Fatalf("dockerfile should use the configured base image:\n%s", dockerfile)
	}
	if !strings.Contains(dockerfile, "COPY package*.json .npmrc ./") {
		t.Fatalf("dockerfile should enforce the project npm config:\n%s", dockerfile)
	}
}
