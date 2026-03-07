package deploy

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sorisltd/eu-deploy/internal/build"
	"github.com/sorisltd/eu-deploy/internal/config"
	"github.com/sorisltd/eu-deploy/internal/detect"
)

func TestNextStandaloneDockerE2E(t *testing.T) {
	if os.Getenv("EUDEPLOY_DOCKER_E2E") != "1" {
		t.Skip("set EUDEPLOY_DOCKER_E2E=1 to run the live Docker smoke test")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker not available: %v", err)
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skipf("npm not available: %v", err)
	}
	if err := dockerDaemonReady(); err != nil {
		t.Skipf("docker daemon not ready: %v", err)
	}

	fixtureDir := filepath.Join("..", "build", "testdata", "next-standalone-app")
	workDir := t.TempDir()

	if err := copyTestTree(fixtureDir, workDir); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	mustRunLiveCommand(t, workDir, "npm", "ci", "--no-audit", "--no-fund")

	d := detect.Detect(workDir)
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

	res, err := build.BuildProject(cfg, workDir)
	if err != nil {
		t.Fatalf("BuildProject: %v", err)
	}

	installDependencies, err := build.RequiresDependencyInstall(cfg, workDir)
	if err != nil {
		t.Fatalf("RequiresDependencyInstall: %v", err)
	}

	hostPort, err := reserveTCPPort()
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}

	suffix := time.Now().UnixNano()
	imageTag := fmt.Sprintf("eu-deploy-next-e2e-%d:local", suffix)
	containerName := SanitizeDockerName(fmt.Sprintf("eu-deploy-next-e2e-%d", suffix))

	opts := DockerOptions{
		WorkDir:             workDir,
		ArtifactPath:        res.ArtifactPath,
		RuntimeStart:        cfg.Runtime.Start,
		ContainerPort:       cfg.Runtime.Port,
		HostPort:            hostPort,
		ImageTag:            imageTag,
		ContainerName:       containerName,
		Detach:              true,
		InstallDependencies: installDependencies,
	}

	defer cleanupDockerResource("container", containerName)
	defer cleanupDockerResource("image", imageTag)

	if err := BuildDockerImage(opts); err != nil {
		t.Fatalf("BuildDockerImage: %v", err)
	}
	if err := RunDockerContainer(opts); err != nil {
		t.Fatalf("RunDockerContainer: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", hostPort)

	html, err := waitForHTTPBody(baseURL+"/", 60*time.Second)
	if err != nil {
		t.Fatalf("root response: %v\ncontainer logs:\n%s", err, dockerLogs(containerName))
	}
	if !strings.Contains(html, "eu-deploy smoke test") {
		t.Fatalf("unexpected root body:\n%s", html)
	}

	robots, err := waitForHTTPBody(baseURL+"/robots.txt", 20*time.Second)
	if err != nil {
		t.Fatalf("robots.txt response: %v\ncontainer logs:\n%s", err, dockerLogs(containerName))
	}
	if !strings.Contains(robots, "Allow: /") {
		t.Fatalf("unexpected robots.txt body:\n%s", robots)
	}

	staticPath := firstStaticAssetPath(html)
	if staticPath == "" {
		t.Fatalf("could not find a Next.js static asset path in root HTML:\n%s", html)
	}

	staticBody, err := waitForHTTPBody(baseURL+staticPath, 20*time.Second)
	if err != nil {
		t.Fatalf("static asset response: %v\nasset path: %s\ncontainer logs:\n%s", err, staticPath, dockerLogs(containerName))
	}
	if strings.TrimSpace(staticBody) == "" {
		t.Fatalf("static asset body was empty for %s", staticPath)
	}
}

func dockerDaemonReady() error {
	cmd := exec.Command("docker", "info", "--format", "{{.ServerVersion}}")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func copyTestTree(srcDir, destDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		destPath := destDir
		if rel != "." {
			destPath = filepath.Join(destDir, rel)
		}

		switch {
		case info.IsDir():
			return os.MkdirAll(destPath, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			return os.Symlink(target, destPath)
		case info.Mode().IsRegular():
			return copyTestFile(path, destPath, info.Mode().Perm())
		default:
			return nil
		}
	})
}

func copyTestFile(srcPath, destPath string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destFile, srcFile); err != nil {
		destFile.Close()
		return err
	}
	return destFile.Close()
}

func mustRunLiveCommand(t *testing.T, dir string, name string, args ...string) {
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

func reserveTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", ln.Addr())
	}
	return addr.Port, nil
}

func waitForHTTPBody(url string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return string(body), nil
		}

		lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
		time.Sleep(500 * time.Millisecond)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("timed out waiting for %s", url)
	}
	return "", lastErr
}

func firstStaticAssetPath(html string) string {
	re := regexp.MustCompile(`/_next/static/[^"' ]+`)
	return re.FindString(html)
}

func cleanupDockerResource(kind string, name string) {
	var cmd *exec.Cmd
	switch kind {
	case "container":
		cmd = exec.Command("docker", "rm", "-f", name)
	case "image":
		cmd = exec.Command("docker", "image", "rm", "-f", name)
	default:
		return
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}

func dockerLogs(containerName string) string {
	cmd := exec.Command("docker", "logs", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return err.Error()
	}
	return string(out)
}
