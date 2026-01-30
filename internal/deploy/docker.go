package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/utils"
)

type DockerOptions struct {
	WorkDir       string
	ArtifactPath  string
	RuntimeStart  string
	ContainerPort int
	HostPort      int
	ImageTag      string
	ContainerName string
	Detach        bool
}

func SanitizeDockerName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "app"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "app"
	}
	return out
}

func BuildDockerImage(opts DockerOptions) error {
	if strings.TrimSpace(opts.WorkDir) == "" {
		return fmt.Errorf("work dir is required")
	}
	if strings.TrimSpace(opts.ArtifactPath) == "" {
		return fmt.Errorf("artifact path is required")
	}
	if strings.TrimSpace(opts.RuntimeStart) == "" {
		return fmt.Errorf("runtime.start is empty in eudeploy.yaml")
	}
	if opts.ContainerPort <= 0 {
		return fmt.Errorf("runtime.port is empty in eudeploy.yaml")
	}
	if strings.TrimSpace(opts.ImageTag) == "" {
		return fmt.Errorf("image tag is required")
	}

	artifactPath := opts.ArtifactPath
	if !filepath.IsAbs(artifactPath) {
		artifactPath = filepath.Join(opts.WorkDir, artifactPath)
	}
	if info, err := os.Stat(artifactPath); err != nil || info.IsDir() {
		return fmt.Errorf("build artifact not found: %s", opts.ArtifactPath)
	}

	if _, err := os.Stat(filepath.Join(opts.WorkDir, "package.json")); err != nil {
		return fmt.Errorf("package.json not found: docker target requires a Node project")
	}

	dockerDir := filepath.Join(opts.WorkDir, ".eudeploy")
	if err := os.MkdirAll(dockerDir, 0o755); err != nil {
		return err
	}
	dockerfilePath := filepath.Join(dockerDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContents(opts)), 0o644); err != nil {
		return err
	}

	buildCmd := fmt.Sprintf("docker build -t %s -f %s .", opts.ImageTag, filepath.ToSlash(filepath.Join(".eudeploy", "Dockerfile")))
	if err := utils.RunShellCommand(buildCmd, opts.WorkDir); err != nil {
		return err
	}

	return nil
}

func RunDockerContainer(opts DockerOptions) error {
	if opts.ContainerPort <= 0 {
		return fmt.Errorf("runtime.port is empty in eudeploy.yaml")
	}
	if opts.HostPort <= 0 {
		opts.HostPort = opts.ContainerPort
	}
	if strings.TrimSpace(opts.ImageTag) == "" {
		return fmt.Errorf("image tag is required")
	}

	containerName := opts.ContainerName
	if strings.TrimSpace(containerName) == "" {
		containerName = "eu-app"
	}

	runArgs := []string{
		"docker run",
	}
	if opts.Detach {
		runArgs = append(runArgs, "-d")
	}
	runArgs = append(runArgs, "--rm")
	runArgs = append(runArgs, "-p", fmt.Sprintf("%d:%d", opts.HostPort, opts.ContainerPort))
	runArgs = append(runArgs, "--name", containerName)
	runArgs = append(runArgs, opts.ImageTag)

	runCmd := strings.Join(runArgs, " ")
	return utils.RunShellCommand(runCmd, opts.WorkDir)
}

func dockerfileContents(opts DockerOptions) string {
	artifactRel := opts.ArtifactPath
	if filepath.IsAbs(artifactRel) {
		if rel, err := filepath.Rel(opts.WorkDir, artifactRel); err == nil {
			artifactRel = rel
		}
	}
	artifactRel = filepath.ToSlash(artifactRel)

	return strings.Join([]string{
		"FROM node:20-slim",
		"WORKDIR /app",
		"ENV NODE_ENV=production",
		"COPY package*.json ./",
		"RUN if [ -f package-lock.json ]; then npm ci --omit=dev; else npm install --omit=dev; fi",
		fmt.Sprintf("ADD %s /app/", artifactRel),
		fmt.Sprintf("EXPOSE %d", opts.ContainerPort),
		fmt.Sprintf("CMD [\"bash\",\"-lc\",%s]", strconv.Quote(opts.RuntimeStart)),
		"",
	}, "\n")
}
