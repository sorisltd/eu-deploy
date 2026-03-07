package deploy

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/config"
)

type HetznerOptions struct {
	WorkDir             string
	ArtifactPath        string
	RuntimeStart        string
	ContainerPort       int
	ServicePort         int
	ImageTag            string
	AppContainerName    string
	ProxyContainerName  string
	InstallDependencies bool
	RemoteHost          string
	RemoteUser          string
	RemotePort          int
	SSHKeyPath          string
	RemoteAppPath       string
	Hostname            string
	RoutePath           string
	HealthcheckPath     string
	Env                 map[string]string
}

func PrepareHetznerConfig(cfg *config.Config, workDir string) (bool, map[string]string, error) {
	p := &linePrompter{
		in:  bufio.NewReader(os.Stdin),
		out: os.Stdout,
	}

	changed := false

	if strings.TrimSpace(cfg.Build.Command) == "" {
		value, err := p.String("Build command", defaultBuildCommand(cfg.Project.Framework), true)
		if err != nil {
			return false, nil, err
		}
		cfg.Build.Command = value
		changed = true
	}
	if strings.TrimSpace(cfg.Build.Output) == "" {
		value, err := p.String("Build output directory", defaultBuildOutput(cfg.Project.Framework), true)
		if err != nil {
			return false, nil, err
		}
		cfg.Build.Output = value
		changed = true
	}
	if strings.TrimSpace(cfg.Runtime.Start) == "" {
		value, err := p.String("Runtime start command", defaultRuntimeStart(cfg.Project.Framework), true)
		if err != nil {
			return false, nil, err
		}
		cfg.Runtime.Start = value
		changed = true
	}
	if cfg.Runtime.Port == 0 {
		value, err := p.Int("Runtime port", 3000, true)
		if err != nil {
			return false, nil, err
		}
		cfg.Runtime.Port = value
		changed = true
	}
	if strings.TrimSpace(cfg.Runtime.Healthcheck.Path) == "" || strings.TrimSpace(cfg.Runtime.Healthcheck.Path) == "/health" {
		value, err := p.String("Healthcheck path", "/", true)
		if err != nil {
			return false, nil, err
		}
		cfg.Runtime.Healthcheck.Path = value
		changed = true
	}

	if len(cfg.Routes) == 0 {
		cfg.Routes = []config.RouteSpec{{Path: "/", Target: "web"}}
		changed = true
	}
	if strings.TrimSpace(cfg.Routes[0].Path) == "" {
		cfg.Routes[0].Path = "/"
		changed = true
	}
	if isPlaceholderHostname(cfg.Routes[0].Hostname) {
		value, err := p.String("Public hostname", "", true)
		if err != nil {
			return false, nil, err
		}
		cfg.Routes[0].Hostname = value
		changed = true
	}

	if cfg.Hetzner == nil {
		cfg.Hetzner = &config.HetznerSpec{}
		changed = true
	}

	if strings.TrimSpace(cfg.Hetzner.Host) == "" {
		value, err := p.String("Hetzner server IP or hostname", "", true)
		if err != nil {
			return false, nil, err
		}
		cfg.Hetzner.Host = value
		changed = true
	}
	if strings.TrimSpace(cfg.Hetzner.User) == "" {
		value, err := p.String("SSH user", "root", true)
		if err != nil {
			return false, nil, err
		}
		cfg.Hetzner.User = value
		changed = true
	}
	if cfg.Hetzner.Port == 0 {
		value, err := p.Int("SSH port", 22, true)
		if err != nil {
			return false, nil, err
		}
		cfg.Hetzner.Port = value
		changed = true
	}
	if strings.TrimSpace(cfg.Hetzner.SSHKeyPath) == "" {
		defaultKey := defaultSSHKeyPath()
		value, err := p.String("SSH key path (leave blank to use ssh defaults)", defaultKey, false)
		if err != nil {
			return false, nil, err
		}
		cfg.Hetzner.SSHKeyPath = value
		changed = true
	}
	if strings.TrimSpace(cfg.Hetzner.AppPath) == "" {
		value, err := p.String("Remote app path", defaultHetznerAppPath(cfg.Hetzner.User, cfg.Project.Name), true)
		if err != nil {
			return false, nil, err
		}
		cfg.Hetzner.AppPath = value
		changed = true
	}
	if cfg.Hetzner.ServicePort == 0 {
		value, err := p.Int("Remote loopback service port", cfg.Runtime.Port, true)
		if err != nil {
			return false, nil, err
		}
		cfg.Hetzner.ServicePort = value
		changed = true
	}

	envValues, envChanged, err := collectDeployEnvValues(cfg, workDir, p)
	if err != nil {
		return false, nil, err
	}
	if envChanged {
		changed = true
	}

	return changed, envValues, nil
}

func DeployToHetzner(opts HetznerOptions) error {
	if err := validateHetznerOptions(opts); err != nil {
		return err
	}

	bundleDir, err := prepareHetznerBundle(opts)
	if err != nil {
		return err
	}
	defer os.RemoveAll(bundleDir)

	if err := ensureRemoteDirectory(opts); err != nil {
		return err
	}
	if err := uploadHetznerBundle(opts, bundleDir); err != nil {
		return err
	}
	if err := runHetznerDeployScript(opts); err != nil {
		return err
	}

	return nil
}

func validateHetznerOptions(opts HetznerOptions) error {
	switch {
	case strings.TrimSpace(opts.WorkDir) == "":
		return fmt.Errorf("work dir is required")
	case strings.TrimSpace(opts.ArtifactPath) == "":
		return fmt.Errorf("artifact path is required")
	case strings.TrimSpace(opts.RuntimeStart) == "":
		return fmt.Errorf("runtime.start is empty in eudeploy.yaml")
	case opts.ContainerPort <= 0:
		return fmt.Errorf("runtime.port is empty in eudeploy.yaml")
	case opts.ServicePort <= 0:
		return fmt.Errorf("hetzner.service_port is required")
	case strings.TrimSpace(opts.RemoteHost) == "":
		return fmt.Errorf("hetzner.host is required")
	case strings.TrimSpace(opts.RemoteUser) == "":
		return fmt.Errorf("hetzner.user is required")
	case opts.RemotePort <= 0:
		return fmt.Errorf("hetzner.port is required")
	case strings.TrimSpace(opts.RemoteAppPath) == "":
		return fmt.Errorf("hetzner.app_path is required")
	case strings.TrimSpace(opts.Hostname) == "":
		return fmt.Errorf("routes[0].hostname is required")
	case strings.TrimSpace(opts.ImageTag) == "":
		return fmt.Errorf("image tag is required")
	case strings.TrimSpace(opts.AppContainerName) == "":
		return fmt.Errorf("app container name is required")
	case strings.TrimSpace(opts.ProxyContainerName) == "":
		return fmt.Errorf("proxy container name is required")
	default:
		return nil
	}
}

func prepareHetznerBundle(opts HetznerOptions) (string, error) {
	bundleDir, err := os.MkdirTemp("", "eudeploy-hetzner-*")
	if err != nil {
		return "", err
	}

	artifactSource := opts.ArtifactPath
	if !filepath.IsAbs(artifactSource) {
		artifactSource = filepath.Join(opts.WorkDir, artifactSource)
	}
	if info, err := os.Stat(artifactSource); err != nil || info.IsDir() {
		os.RemoveAll(bundleDir)
		return "", fmt.Errorf("build artifact not found: %s", opts.ArtifactPath)
	}

	if err := copyHetznerFile(artifactSource, filepath.Join(bundleDir, "artifact.tar.gz")); err != nil {
		os.RemoveAll(bundleDir)
		return "", err
	}

	dockerfileOpts := DockerOptions{
		WorkDir:             bundleDir,
		ArtifactPath:        "artifact.tar.gz",
		RuntimeStart:        opts.RuntimeStart,
		ContainerPort:       opts.ContainerPort,
		InstallDependencies: opts.InstallDependencies,
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "Dockerfile"), []byte(dockerfileContents(dockerfileOpts)), 0o644); err != nil {
		os.RemoveAll(bundleDir)
		return "", err
	}

	caddyfile := renderCaddyfile(opts.Hostname, opts.RoutePath, opts.ServicePort)
	if err := os.WriteFile(filepath.Join(bundleDir, "Caddyfile"), []byte(caddyfile), 0o644); err != nil {
		os.RemoveAll(bundleDir)
		return "", err
	}

	envFile, err := renderEnvFile(opts.Env)
	if err != nil {
		os.RemoveAll(bundleDir)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "app.env"), []byte(envFile), 0o600); err != nil {
		os.RemoveAll(bundleDir)
		return "", err
	}

	if opts.InstallDependencies {
		if err := copyIfPresent(filepath.Join(opts.WorkDir, "package.json"), filepath.Join(bundleDir, "package.json")); err != nil {
			os.RemoveAll(bundleDir)
			return "", err
		}
		if err := copyIfPresent(filepath.Join(opts.WorkDir, "package-lock.json"), filepath.Join(bundleDir, "package-lock.json")); err != nil {
			os.RemoveAll(bundleDir)
			return "", err
		}
	}

	return bundleDir, nil
}

func ensureRemoteDirectory(opts HetznerOptions) error {
	script := fmt.Sprintf("mkdir -p %s\n", shellQuote(opts.RemoteAppPath))
	return runRemoteScript(opts, script)
}

func uploadHetznerBundle(opts HetznerOptions, bundleDir string) error {
	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		return err
	}

	args := []string{}
	if opts.RemotePort > 0 {
		args = append(args, "-P", strconv.Itoa(opts.RemotePort))
	}
	if strings.TrimSpace(opts.SSHKeyPath) != "" {
		args = append(args, "-i", opts.SSHKeyPath)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		args = append(args, filepath.Join(bundleDir, entry.Name()))
	}
	args = append(args, fmt.Sprintf("%s@%s:%s/", opts.RemoteUser, opts.RemoteHost, opts.RemoteAppPath))

	cmd := exec.Command("scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runHetznerDeployScript(opts HetznerOptions) error {
	healthPath := strings.TrimSpace(opts.HealthcheckPath)
	if healthPath == "" {
		healthPath = "/"
	}
	if !strings.HasPrefix(healthPath, "/") {
		healthPath = "/" + healthPath
	}

	script := strings.Join([]string{
		"set -euo pipefail",
		fmt.Sprintf("cd %s", shellQuote(opts.RemoteAppPath)),
		"if ! command -v docker >/dev/null 2>&1; then",
		"  echo 'docker is required on the remote host' >&2",
		"  exit 1",
		"fi",
		fmt.Sprintf("docker build -t %s -f Dockerfile .", shellQuote(opts.ImageTag)),
		fmt.Sprintf("docker rm -f %s >/dev/null 2>&1 || true", shellQuote(opts.AppContainerName)),
		fmt.Sprintf("docker run -d --restart unless-stopped --env-file app.env --name %s -p 127.0.0.1:%d:%d %s >/dev/null",
			shellQuote(opts.AppContainerName), opts.ServicePort, opts.ContainerPort, shellQuote(opts.ImageTag)),
		"attempt=0",
		fmt.Sprintf("until docker run --rm --network host curlimages/curl:8.12.1 -fsS %s >/dev/null 2>&1; do",
			shellQuote("http://127.0.0.1:"+strconv.Itoa(opts.ServicePort)+healthPath)),
		"  attempt=$((attempt + 1))",
		"  if [ \"$attempt\" -ge 30 ]; then",
		fmt.Sprintf("    docker logs %s || true", shellQuote(opts.AppContainerName)),
		"    exit 1",
		"  fi",
		"  sleep 2",
		"done",
		"docker pull caddy:2 >/dev/null",
		fmt.Sprintf("docker rm -f %s >/dev/null 2>&1 || true", shellQuote(opts.ProxyContainerName)),
		fmt.Sprintf("docker run -d --restart unless-stopped --network host --name %s -v %s:/etc/caddy/Caddyfile:ro -v %s:/data -v %s:/config caddy:2 >/dev/null",
			shellQuote(opts.ProxyContainerName),
			shellQuote(filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "Caddyfile"))),
			shellQuote(filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "caddy-data"))),
			shellQuote(filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "caddy-config")))),
	}, "\n")

	return runRemoteScript(opts, script)
}

func runRemoteScript(opts HetznerOptions, script string) error {
	args := []string{}
	if opts.RemotePort > 0 {
		args = append(args, "-p", strconv.Itoa(opts.RemotePort))
	}
	if strings.TrimSpace(opts.SSHKeyPath) != "" {
		args = append(args, "-i", opts.SSHKeyPath)
	}
	args = append(args, fmt.Sprintf("%s@%s", opts.RemoteUser, opts.RemoteHost), "bash -se")

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func renderCaddyfile(hostname, routePath string, servicePort int) string {
	hostname = strings.TrimSpace(hostname)
	routePath = normalizeRoutePath(routePath)

	lines := []string{
		fmt.Sprintf("%s {", hostname),
		"  encode zstd gzip",
	}

	if routePath == "/" {
		lines = append(lines, fmt.Sprintf("  reverse_proxy 127.0.0.1:%d", servicePort))
	} else {
		lines = append(lines,
			fmt.Sprintf("  handle_path %s* {", routePath),
			fmt.Sprintf("    reverse_proxy 127.0.0.1:%d", servicePort),
			"  }",
			"  respond 404",
		)
	}
	lines = append(lines, "}")

	return strings.Join(lines, "\n") + "\n"
}

func renderEnvFile(values map[string]string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		value := values[key]
		if strings.ContainsAny(value, "\x00\r\n") {
			return "", fmt.Errorf("env value for %s contains unsupported newline or null bytes", key)
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(value)
		b.WriteByte('\n')
	}

	return b.String(), nil
}

func collectDeployEnvValues(cfg *config.Config, workDir string, p *linePrompter) (map[string]string, bool, error) {
	changed := false

	if len(cfg.Env.Public) == 0 && len(cfg.Env.Secret) == 0 {
		if keys, err := parseEnvTemplateKeys(filepath.Join(workDir, ".env.example")); err == nil && len(keys) > 0 {
			useKeys, err := p.Bool(fmt.Sprintf("Found %d variables in .env.example. Add them as deployment secrets", len(keys)), true)
			if err != nil {
				return nil, false, err
			}
			if useKeys {
				if cfg.Env.Secret == nil {
					cfg.Env.Secret = map[string]string{}
				}
				for _, key := range keys {
					if _, ok := cfg.Env.Secret[key]; !ok {
						cfg.Env.Secret[key] = ""
						changed = true
					}
				}
			}
		}
	}

	values := map[string]string{}

	for _, key := range sortedKeys(cfg.Env.Public) {
		value := strings.TrimSpace(cfg.Env.Public[key])
		if value == "" {
			var err error
			value, err = p.String(fmt.Sprintf("Value for public env %s (leave blank to skip)", key), "", false)
			if err != nil {
				return nil, false, err
			}
		}
		if value != "" {
			values[key] = value
		}
	}

	for _, key := range sortedKeys(cfg.Env.Secret) {
		value := strings.TrimSpace(cfg.Env.Secret[key])
		if value == "" {
			var err error
			value, err = p.String(fmt.Sprintf("Value for secret env %s (leave blank to skip)", key), "", false)
			if err != nil {
				return nil, false, err
			}
		}
		if value != "" {
			values[key] = value
		}
	}

	return values, changed, nil
}

func parseEnvTemplateKeys(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var keys []string

	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		keys = append(keys, name)
	}

	sort.Strings(keys)
	return keys, nil
}

func normalizeRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func isPlaceholderHostname(host string) bool {
	host = strings.TrimSpace(host)
	return host == "" || strings.HasSuffix(host, ".eu-deploy.dev")
}

func defaultBuildCommand(framework string) string {
	if strings.TrimSpace(framework) == "" {
		return "npm run build"
	}
	return "npm run build"
}

func defaultBuildOutput(framework string) string {
	switch strings.TrimSpace(framework) {
	case "nextjs":
		return ".next"
	case "solidstart", "nuxt":
		return ".output"
	case "sveltekit":
		return "build"
	default:
		return "dist"
	}
}

func defaultRuntimeStart(framework string) string {
	switch strings.TrimSpace(framework) {
	case "nextjs":
		return "npm run start"
	case "solidstart", "nuxt":
		return "node .output/server/index.mjs"
	case "sveltekit":
		return "node build"
	default:
		return "npm run start"
	}
}

func defaultSSHKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	for _, candidate := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(home, ".ssh", candidate)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func defaultHetznerAppPath(user, projectName string) string {
	safeProject := SanitizeDockerName(projectName)
	if safeProject == "" {
		safeProject = "app"
	}
	if strings.TrimSpace(user) == "" || strings.TrimSpace(user) == "root" {
		return filepath.ToSlash(filepath.Join("/opt", "eu-deploy", safeProject))
	}
	return filepath.ToSlash(filepath.Join("/home", user, "eu-deploy", safeProject))
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyIfPresent(srcPath, destPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("expected file, got directory: %s", srcPath)
	}
	return copyHetznerFile(srcPath, destPath)
}

func copyHetznerFile(srcPath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return err
	}

	dest, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dest, src); err != nil {
		dest.Close()
		return err
	}
	return dest.Close()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

type linePrompter struct {
	in  *bufio.Reader
	out io.Writer
}

func (p *linePrompter) String(label, defaultValue string, required bool) (string, error) {
	for {
		if defaultValue != "" {
			if _, err := fmt.Fprintf(p.out, "%s [%s]: ", label, defaultValue); err != nil {
				return "", err
			}
		} else {
			if _, err := fmt.Fprintf(p.out, "%s: ", label); err != nil {
				return "", err
			}
		}

		line, err := p.in.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}

		value := strings.TrimSpace(line)
		if value == "" {
			value = defaultValue
		}
		if value != "" || !required {
			return value, nil
		}

		if _, err := fmt.Fprintln(p.out, "A value is required."); err != nil {
			return "", err
		}
	}
}

func (p *linePrompter) Int(label string, defaultValue int, required bool) (int, error) {
	defaultString := ""
	if defaultValue > 0 {
		defaultString = strconv.Itoa(defaultValue)
	}

	for {
		value, err := p.String(label, defaultString, required)
		if err != nil {
			return 0, err
		}
		if value == "" && !required {
			return 0, nil
		}
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			return parsed, nil
		}

		if _, err := fmt.Fprintln(p.out, "Enter a valid positive integer."); err != nil {
			return 0, err
		}
	}
}

func (p *linePrompter) Bool(label string, defaultValue bool) (bool, error) {
	defaultString := "y"
	if !defaultValue {
		defaultString = "n"
	}

	for {
		value, err := p.String(label+" [y/n]", defaultString, true)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			if _, err := fmt.Fprintln(p.out, "Enter y or n."); err != nil {
				return false, err
			}
		}
	}
}
