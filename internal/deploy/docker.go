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
	WorkDir             string
	ArtifactPath        string
	PostDeployArchive   string
	RuntimeStart        string
	ContainerPort       int
	HostPort            int
	ImageTag            string
	ContainerName       string
	Detach              bool
	InstallDependencies bool
	BaseImage           string
	HasNPMConfig        bool
	Packages            []string
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
	if strings.TrimSpace(opts.PostDeployArchive) != "" {
		postDeployArchive := opts.PostDeployArchive
		if !filepath.IsAbs(postDeployArchive) {
			postDeployArchive = filepath.Join(opts.WorkDir, postDeployArchive)
		}
		if info, err := os.Stat(postDeployArchive); err != nil || info.IsDir() {
			return fmt.Errorf("post-deploy archive not found: %s", opts.PostDeployArchive)
		}
	}

	if opts.InstallDependencies {
		if _, err := os.Stat(filepath.Join(opts.WorkDir, "package.json")); err != nil {
			return fmt.Errorf("package.json not found: docker target requires a Node project")
		}
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

	postDeployRel := opts.PostDeployArchive
	if filepath.IsAbs(postDeployRel) {
		if rel, err := filepath.Rel(opts.WorkDir, postDeployRel); err == nil {
			postDeployRel = rel
		}
	}
	postDeployRel = filepath.ToSlash(postDeployRel)

	lines := []string{
		"FROM " + dockerBaseImage(opts.BaseImage),
		dockerfilePackagesStep(opts.Packages),
		"WORKDIR /app",
		"ENV NODE_ENV=production",
		dockerfileInstallStep(opts.InstallDependencies, opts.HasNPMConfig),
		fmt.Sprintf("ADD %s /app/", artifactRel),
	}
	if strings.TrimSpace(postDeployRel) != "" {
		lines = append(lines, fmt.Sprintf("ADD %s /app/", postDeployRel))
	}
	lines = append(lines,
		fmt.Sprintf("EXPOSE %d", opts.ContainerPort),
		fmt.Sprintf("CMD [\"bash\",\"-lc\",%s]", strconv.Quote(opts.RuntimeStart)),
		"",
	)
	return strings.Join(lines, "\n")
}

func dockerfilePackagesStep(packages []string) string {
	if len(packages) == 0 {
		return ""
	}
	return fmt.Sprintf("RUN apt-get update -qq && apt-get install -y --no-install-recommends %s && rm -rf /var/lib/apt/lists/*", strings.Join(packages, " "))
}

func dockerBaseImage(image string) string {
	if strings.TrimSpace(image) == "" {
		return "node:20-slim"
	}
	return strings.TrimSpace(image)
}

func dockerfileInstallStep(installDependencies, hasNPMConfig bool) string {
	if !installDependencies {
		return ""
	}
	copyManifest := "COPY package*.json ./"
	if hasNPMConfig {
		copyManifest = "COPY package*.json .npmrc ./"
	}
	return strings.Join([]string{
		copyManifest,
		"RUN if [ -f package-lock.json ]; then npm ci --omit=dev; else npm install --omit=dev; fi",
	}, "\n")
}
