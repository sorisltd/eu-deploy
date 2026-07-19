package deploy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/build"
	"github.com/sorisltd/eu-deploy/internal/config"
)

const (
	sharedProxyContainer    = "eu-shared-caddy"
	sharedProxyDirName      = "_proxy"
	sharedDockerNetwork     = "eu-deploy"
	sharedPostgresDirName   = "_postgres"
	sharedPostgresContainer = "eu-shared-postgres"
	defaultReleaseKeepCount = 3
)

type RemoteOptions struct {
	Provider            RemoteTarget
	ProjectName         string
	RuntimeType         string
	WorkDir             string
	ArtifactPath        string
	ArtifactSHA         string
	ReleaseID           string
	RuntimeStart        string
	StaticArchiveRoot   string
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
	RemoteServerPath    string
	RemoteAppPath       string
	Hostname            string
	Hostnames           []string
	RoutePath           string
	Routes              []RemoteRoute
	AnalyticsLogName    string
	HealthcheckPath     string
	SiteConfigName      string
	SharedDatabase      *SharedDatabaseOptions
	PostDeploy          *PostDeployOptions
	Env                 map[string]string
	KeepReleases        int
	Packages            []string
	Volumes             []string
}

type RemoteRoute struct {
	Hostnames      []string
	Path           string
	PreservePrefix bool
	Redirect       string
	Code           int
	CaddyExtra     string
}

type HetznerOptions = RemoteOptions

type SharedDatabaseOptions struct {
	Version  string
	Name     string
	User     string
	Password string
}

type PostDeployOptions struct {
	Command string
	Include []string
}

type PrepareRemoteConfigOptions struct {
	PromptEnv bool
}

type PrepareRemoteResult struct {
	Changed                bool
	EnvValues              map[string]string
	SharedDatabasePassword string
}

type PrepareHetznerConfigOptions = PrepareRemoteConfigOptions
type PrepareHetznerResult = PrepareRemoteResult

type RemoteDeployHooks struct {
	OnPhase func(phaseID string)
}

func PrepareRemoteConfig(cfg *config.Config, workDir string, target RemoteTarget, options PrepareRemoteConfigOptions) (PrepareRemoteResult, error) {
	p := &linePrompter{
		in:  bufio.NewReader(os.Stdin),
		out: os.Stdout,
	}
	runtimeType := normalizedRuntimeType(cfg.Runtime.Type)

	changed := false
	if cfg.Deploy.Provider == "" {
		cfg.Deploy.Provider = string(target)
		changed = true
	}

	spec := EnsureRemoteProviderSpec(cfg, target)
	if spec == nil {
		return PrepareRemoteResult{}, fmt.Errorf("%s config is missing", target)
	}
	providerLabel := RemoteTargetLabel(target)

	if strings.TrimSpace(cfg.Build.Command) == "" {
		value, err := p.String("Build command", defaultBuildCommand(cfg.Project.Framework), true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		cfg.Build.Command = value
		changed = true
	}
	if runtimeType == "static" && strings.TrimSpace(cfg.Build.Output) == "" && strings.TrimSpace(cfg.Runtime.Output) != "" {
		cfg.Build.Output = cfg.Runtime.Output
		changed = true
	}
	if strings.TrimSpace(cfg.Build.Output) == "" {
		value, err := p.String("Build output directory", defaultBuildOutput(cfg.Project.Framework), true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		cfg.Build.Output = value
		changed = true
	}

	if runtimeType == "static" {
		if strings.TrimSpace(cfg.Runtime.Output) == "" {
			cfg.Runtime.Output = config.EffectiveBuildOutput(*cfg)
			if strings.TrimSpace(cfg.Runtime.Output) == "" {
				value, err := p.String("Static output directory", defaultBuildOutput(cfg.Project.Framework), true)
				if err != nil {
					return PrepareRemoteResult{}, err
				}
				cfg.Runtime.Output = value
			}
			changed = true
		}
	} else {
		if strings.TrimSpace(cfg.Runtime.Start) == "" {
			value, err := p.String("Runtime start command", defaultRuntimeStart(cfg.Project.Framework), true)
			if err != nil {
				return PrepareRemoteResult{}, err
			}
			cfg.Runtime.Start = value
			changed = true
		}
		if cfg.Runtime.Port == 0 {
			value, err := p.Int("Runtime port", 3000, true)
			if err != nil {
				return PrepareRemoteResult{}, err
			}
			cfg.Runtime.Port = value
			changed = true
		}
		if strings.TrimSpace(cfg.Runtime.Healthcheck.Path) == "" || strings.TrimSpace(cfg.Runtime.Healthcheck.Path) == "/health" {
			value, err := p.String("Healthcheck path", "/", true)
			if err != nil {
				return PrepareRemoteResult{}, err
			}
			cfg.Runtime.Healthcheck.Path = value
			changed = true
		}
	}

	if len(cfg.Routes) == 0 {
		cfg.Routes = []config.RouteSpec{{Path: "/", Target: "web"}}
		changed = true
	}
	if strings.TrimSpace(cfg.Routes[0].Path) == "" {
		cfg.Routes[0].Path = "/"
		changed = true
	}
	if len(cfg.Routes[0].Hostnames) == 0 && isPlaceholderHostname(cfg.Routes[0].Hostname) {
		value, err := p.String("Public hostname", "", true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		cfg.Routes[0].Hostname = value
		changed = true
	}

	if strings.TrimSpace(spec.Host) == "" {
		value, err := p.String(fmt.Sprintf("%s server IP or hostname", providerLabel), "", true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		spec.Host = value
		changed = true
	}
	if strings.TrimSpace(spec.User) == "" {
		value, err := p.String("SSH user", "root", true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		spec.User = value
		changed = true
	}
	if spec.Port == 0 {
		value, err := p.Int("SSH port", 22, true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		spec.Port = value
		changed = true
	}
	if strings.TrimSpace(spec.SSHKeyPath) == "" {
		defaultKey := defaultSSHKeyPath()
		value, err := p.String("SSH key path (leave blank to use ssh defaults)", defaultKey, false)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		spec.SSHKeyPath = value
		changed = true
	}

	serverPath := strings.TrimSpace(spec.ServerPath)
	if serverPath == "" {
		serverPath = effectiveRemoteServerPath(*spec, cfg.Project.Name)
		value, err := p.String("Remote server root", serverPath, true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		spec.ServerPath = filepath.ToSlash(filepath.Clean(value))
		changed = true
	}

	if strings.TrimSpace(spec.AppPath) == "" {
		value, err := p.String("Remote app path", defaultRemoteAppPath(spec.ServerPath, cfg.Project.Name), true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		spec.AppPath = filepath.ToSlash(filepath.Clean(value))
		changed = true
	}
	if runtimeType != "static" && spec.ServicePort == 0 {
		value, err := p.Int("Remote loopback service port", cfg.Runtime.Port, true)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		spec.ServicePort = value
		changed = true
	}

	databaseChanged, err := prepareDatabaseConfig(cfg, workDir, p)
	if err != nil {
		return PrepareRemoteResult{}, err
	}
	if databaseChanged {
		changed = true
	}

	var envValues map[string]string
	sharedDatabasePassword := ""
	if options.PromptEnv {
		var envChanged bool
		envValues, envChanged, err = collectDeployEnvValues(cfg, workDir, p)
		if err != nil {
			return PrepareRemoteResult{}, err
		}
		if envChanged {
			changed = true
		}
		if usesSharedDatabase(cfg.Database) {
			sharedDatabasePassword, err = promptSharedDatabasePassword(p, cfg.Database.Shared.User)
			if err != nil {
				return PrepareRemoteResult{}, err
			}
		}
	}

	return PrepareRemoteResult{
		Changed:                changed,
		EnvValues:              envValues,
		SharedDatabasePassword: sharedDatabasePassword,
	}, nil
}

func PrepareHetznerConfig(cfg *config.Config, workDir string, options PrepareHetznerConfigOptions) (PrepareHetznerResult, error) {
	return PrepareRemoteConfig(cfg, workDir, RemoteTargetHetzner, options)
}

func DeployToRemote(opts RemoteOptions) error {
	return DeployToRemoteWithHooks(opts, RemoteDeployHooks{})
}

func DeployToRemoteWithHooks(opts RemoteOptions, hooks RemoteDeployHooks) error {
	if err := validateRemoteDeployOptions(opts); err != nil {
		return err
	}

	if hooks.OnPhase != nil {
		hooks.OnPhase("uploadRelease")
	}
	bundleDir, err := prepareHetznerBundle(opts)
	if err != nil {
		return err
	}
	defer os.RemoveAll(bundleDir)

	if err := ensureRemoteDirectories(opts); err != nil {
		return err
	}
	if err := uploadHetznerBundle(opts, bundleDir); err != nil {
		return err
	}
	if hooks.OnPhase != nil {
		hooks.OnPhase("activateRelease")
	}
	if err := runHetznerDeployScript(opts); err != nil {
		return err
	}

	return nil
}

func DeployToHetzner(opts HetznerOptions) error {
	return DeployToRemote(opts)
}

func validateRemoteOptions(opts RemoteOptions) error {
	prefix := providerConfigFieldPrefix(opts.Provider)
	staticRuntime := isStaticRuntime(opts.RuntimeType)
	switch {
	case strings.TrimSpace(opts.WorkDir) == "":
		return fmt.Errorf("work dir is required")
	case !staticRuntime && opts.ServicePort <= 0:
		return fmt.Errorf("%s.service_port is required", prefix)
	case strings.TrimSpace(opts.RemoteHost) == "":
		return fmt.Errorf("%s.host is required", prefix)
	case strings.TrimSpace(opts.RemoteUser) == "":
		return fmt.Errorf("%s.user is required", prefix)
	case opts.RemotePort <= 0:
		return fmt.Errorf("%s.port is required", prefix)
	case strings.TrimSpace(opts.RemoteServerPath) == "":
		return fmt.Errorf("%s.server_path is required", prefix)
	case strings.TrimSpace(opts.RemoteAppPath) == "":
		return fmt.Errorf("%s.app_path is required", prefix)
	case strings.TrimSpace(opts.Hostname) == "":
		return fmt.Errorf("routes[0].hostname is required")
	case !staticRuntime && strings.TrimSpace(opts.ImageTag) == "":
		return fmt.Errorf("image tag is required")
	case !staticRuntime && strings.TrimSpace(opts.AppContainerName) == "":
		return fmt.Errorf("app container name is required")
	case strings.TrimSpace(opts.ProxyContainerName) == "":
		return fmt.Errorf("proxy container name is required")
	case strings.TrimSpace(opts.SiteConfigName) == "":
		return fmt.Errorf("site config name is required")
	case opts.SharedDatabase != nil && strings.TrimSpace(opts.SharedDatabase.Version) == "":
		return fmt.Errorf("database.shared.version is required")
	case opts.SharedDatabase != nil && strings.TrimSpace(opts.SharedDatabase.Name) == "":
		return fmt.Errorf("database.shared.name is required")
	case opts.SharedDatabase != nil && strings.TrimSpace(opts.SharedDatabase.User) == "":
		return fmt.Errorf("database.shared.user is required")
	default:
		return nil
	}
}

func validateRemoteDeployOptions(opts RemoteOptions) error {
	if err := validateRemoteOptions(opts); err != nil {
		return err
	}
	if isStaticRuntime(opts.RuntimeType) {
		switch {
		case strings.TrimSpace(opts.ArtifactPath) == "":
			return fmt.Errorf("artifact path is required")
		case strings.TrimSpace(opts.StaticArchiveRoot) == "":
			return fmt.Errorf("runtime.output or build.output is required for runtime.type=static")
		default:
			return nil
		}
	}
	switch {
	case strings.TrimSpace(opts.ArtifactPath) == "":
		return fmt.Errorf("artifact path is required")
	case strings.TrimSpace(opts.RuntimeStart) == "":
		return fmt.Errorf("runtime.start is empty in eudeploy.yaml")
	case opts.ContainerPort <= 0:
		return fmt.Errorf("runtime.port is empty in eudeploy.yaml")
	default:
		return nil
	}
}

func prepareHetznerBundle(opts RemoteOptions) (string, error) {
	bundleDir, err := os.MkdirTemp("", "eudeploy-remote-*")
	if err != nil {
		return "", err
	}

	artifactSource := opts.ArtifactPath
	if !filepath.IsAbs(artifactSource) {
		artifactSource = filepath.Join(opts.WorkDir, opts.ArtifactPath)
	}
	if info, err := os.Stat(artifactSource); err != nil || info.IsDir() {
		os.RemoveAll(bundleDir)
		return "", fmt.Errorf("build artifact not found: %s", opts.ArtifactPath)
	}

	if err := copyHetznerFile(artifactSource, filepath.Join(bundleDir, "artifact.tar.gz")); err != nil {
		os.RemoveAll(bundleDir)
		return "", err
	}

	if isStaticRuntime(opts.RuntimeType) {
		return bundleDir, nil
	}

	dockerfileOpts := DockerOptions{
		WorkDir:             bundleDir,
		ArtifactPath:        "artifact.tar.gz",
		RuntimeStart:        opts.RuntimeStart,
		ContainerPort:       opts.ContainerPort,
		InstallDependencies: opts.InstallDependencies,
		Packages:            opts.Packages,
	}
	if opts.PostDeploy != nil && len(opts.PostDeploy.Include) > 0 {
		postDeployArchivePath := filepath.Join(bundleDir, "postdeploy.tar.gz")
		if err := build.PackagePaths(opts.WorkDir, opts.PostDeploy.Include, postDeployArchivePath); err != nil {
			os.RemoveAll(bundleDir)
			return "", err
		}
		dockerfileOpts.PostDeployArchive = "postdeploy.tar.gz"
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "Dockerfile"), []byte(dockerfileContents(dockerfileOpts)), 0o644); err != nil {
		os.RemoveAll(bundleDir)
		return "", err
	}

	siteCaddy := renderSiteCaddyfile(opts.Hostnames, opts.RoutePath, opts.ServicePort, opts.AnalyticsLogName)
	if err := os.WriteFile(filepath.Join(bundleDir, "site.caddy"), []byte(siteCaddy), 0o644); err != nil {
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

func ensureRemoteDirectories(opts RemoteOptions) error {
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("mkdir -p %s", shellQuote(opts.RemoteServerPath)),
		fmt.Sprintf("mkdir -p %s", shellQuote(opts.RemoteAppPath)),
		fmt.Sprintf("mkdir -p %s", shellQuote(filepath.ToSlash(filepath.Join(proxyRoot, "sites")))),
		fmt.Sprintf("mkdir -p %s", shellQuote(filepath.ToSlash(filepath.Join(proxyRoot, "data")))),
		fmt.Sprintf("mkdir -p %s", shellQuote(filepath.ToSlash(filepath.Join(proxyRoot, "config")))),
	}
	if opts.SharedDatabase != nil {
		postgresRoot := sharedPostgresRoot(opts.RemoteServerPath)
		lines = append(lines,
			fmt.Sprintf("mkdir -p %s", shellQuote(postgresRoot)),
			fmt.Sprintf("mkdir -p %s", shellQuote(filepath.ToSlash(filepath.Join(postgresRoot, "data")))),
		)
	}
	script := strings.Join(lines, "\n")
	return runRemoteScript(opts, script)
}

func uploadHetznerBundle(opts RemoteOptions, bundleDir string) error {
	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		return err
	}

	args := buildSCPArgs(opts)
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

func runHetznerDeployScript(opts RemoteOptions) error {
	script := renderHetznerDeployScript(opts)
	return runRemoteScript(opts, script)
}

func renderHetznerDeployScript(opts RemoteOptions) string {
	if isStaticRuntime(opts.RuntimeType) {
		return renderStaticHetznerDeployScript(opts)
	}
	if strings.TrimSpace(opts.ReleaseID) == "" {
		opts.ReleaseID = "release"
	}
	if strings.TrimSpace(opts.ArtifactSHA) == "" {
		opts.ArtifactSHA = "unknown"
	}
	healthPath := normalizedHealthcheckPath(opts.HealthcheckPath)
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	proxySitesDir := filepath.ToSlash(filepath.Join(proxyRoot, "sites"))
	rootCaddyPath := filepath.ToSlash(filepath.Join(proxyRoot, "Caddyfile"))
	proxyDataPath := filepath.ToSlash(filepath.Join(proxyRoot, "data"))
	proxyConfigPath := filepath.ToSlash(filepath.Join(proxyRoot, "config"))
	siteConfigPath := filepath.ToSlash(filepath.Join(proxySitesDir, opts.SiteConfigName))
	runtimeEnvPath := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "app.runtime.env"))
	releaseDir := releaseDirPath(opts, opts.ReleaseID)
	releasesPath := releasesRootPath(opts)
	historyPath := releaseHistoryPath(opts)
	activeSlotFile := activeSlotPath(opts)
	currentReleaseFile := currentReleasePath(opts)
	primaryPort, secondaryPort := releaseSlotPorts(opts.ServicePort)
	imageRepo := releaseImageRepository(opts.ImageTag)
	releaseImage := releaseImageTag(opts, opts.ReleaseID)
	nextSiteCaddy := renderSiteCaddyfileWithUpstream(opts.Hostnames, opts.RoutePath, "127.0.0.1:${TARGET_PORT}", opts.AnalyticsLogName)
	cleanup := renderReleaseCleanupCommands(opts, "history_limit")
	metadataPath := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "analytics-project.json"))
	metadataContents := renderAnalyticsProjectMetadata(opts)
	siteConfigMarker := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, ".site-config"))

	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("cd %s", shellQuote(opts.RemoteAppPath)),
		"if ! command -v docker >/dev/null 2>&1; then",
		"  echo 'docker is required on the remote host' >&2",
		"  exit 1",
		"fi",
		fmt.Sprintf("docker network inspect %s >/dev/null 2>&1 || docker network create %s >/dev/null",
			shellQuote(sharedDockerNetwork),
			shellQuote(sharedDockerNetwork)),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(rootCaddyPath), renderRootCaddyfile()),
		fmt.Sprintf("if [ -f %s ]; then", shellQuote(siteConfigMarker)),
		fmt.Sprintf("  prev_site_config=\"$(tr -d '\\r\\n' < %s)\"", shellQuote(siteConfigMarker)),
		fmt.Sprintf("  if [ -n \"$prev_site_config\" ] && [ \"$prev_site_config\" != %s ]; then", shellQuote(opts.SiteConfigName)),
		fmt.Sprintf("    rm -f %s/\"$prev_site_config\"", shellQuote(proxySitesDir)),
		"  fi",
		"fi",
		fmt.Sprintf("printf '%%s\\n' %s > %s", shellQuote(opts.SiteConfigName), shellQuote(siteConfigMarker)),
		"grep -v '^[^=]*=$' app.env > app.env.nonempty || true",
		fmt.Sprintf("if [ -f %s ]; then", shellQuote(runtimeEnvPath)),
		"  while IFS= read -r line || [ -n \"$line\" ]; do",
		"    key=\"${line%%=*}\"",
		"    [ -z \"$key\" ] && continue",
		"    case \"$key\" in \\#*) continue;; esac",
		"    grep -qx \"${key}=.*\" app.env.nonempty 2>/dev/null || printf '%s\\n' \"$line\" >> app.env.merged",
		fmt.Sprintf("  done < %s", shellQuote(runtimeEnvPath)),
		"  cat app.env.nonempty >> app.env.merged 2>/dev/null || true",
		fmt.Sprintf("  mv app.env.merged %s", shellQuote(runtimeEnvPath)),
		"else",
		fmt.Sprintf("  cp app.env %s", shellQuote(runtimeEnvPath)),
		"fi",
		"rm -f app.env.nonempty",
		fmt.Sprintf("mkdir -p %s", shellQuote(releasesPath)),
		fmt.Sprintf("mkdir -p %s", shellQuote(releaseDir)),
		fmt.Sprintf("mkdir -p %s", shellQuote(filepath.Dir(metadataPath))),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(metadataPath), metadataContents),
		fmt.Sprintf("cp artifact.tar.gz %s", shellQuote(filepath.ToSlash(filepath.Join(releaseDir, "artifact.tar.gz")))),
		fmt.Sprintf("cp Dockerfile %s", shellQuote(filepath.ToSlash(filepath.Join(releaseDir, "Dockerfile")))),
		fmt.Sprintf("cp app.env %s", shellQuote(filepath.ToSlash(filepath.Join(releaseDir, "app.env")))),
	}
	if opts.InstallDependencies {
		lines = append(lines,
			fmt.Sprintf("if [ -f package.json ]; then cp package.json %s; fi", shellQuote(filepath.ToSlash(filepath.Join(releaseDir, "package.json")))),
			fmt.Sprintf("if [ -f package-lock.json ]; then cp package-lock.json %s; fi", shellQuote(filepath.ToSlash(filepath.Join(releaseDir, "package-lock.json")))),
		)
	}
	if opts.PostDeploy != nil && len(opts.PostDeploy.Include) > 0 {
		lines = append(lines,
			fmt.Sprintf("if [ -f postdeploy.tar.gz ]; then cp postdeploy.tar.gz %s; fi", shellQuote(filepath.ToSlash(filepath.Join(releaseDir, "postdeploy.tar.gz")))),
		)
	}

	if opts.SharedDatabase != nil {
		lines = append(lines, renderSharedDatabaseSetup(opts, runtimeEnvPath)...)
	}

	lines = append(lines,
		fmt.Sprintf("IMAGE_REPO=%s", shellQuote(imageRepo)),
		fmt.Sprintf("RELEASE_IMAGE=%s", shellQuote(releaseImage)),
		fmt.Sprintf("PRIMARY_PORT=%d", primaryPort),
		fmt.Sprintf("SECONDARY_PORT=%d", secondaryPort),
		fmt.Sprintf("ACTIVE_SLOT_FILE=%s", shellQuote(activeSlotFile)),
		fmt.Sprintf("CURRENT_RELEASE_FILE=%s", shellQuote(currentReleaseFile)),
		fmt.Sprintf("HISTORY_FILE=%s", shellQuote(historyPath)),
		fmt.Sprintf("APP_CONTAINER_BASE=%s", shellQuote(opts.AppContainerName)),
		"active_slot=''",
		`if [ -f "$ACTIVE_SLOT_FILE" ]; then active_slot="$(tr -d '\r\n' < "$ACTIVE_SLOT_FILE")"; fi`,
		`legacy_container="$APP_CONTAINER_BASE"`,
		`if [ -z "$active_slot" ] && docker ps --format '{{.Names}}' | grep -Fx -- "${APP_CONTAINER_BASE}-a" >/dev/null 2>&1; then`,
		`  active_slot='a'`,
		"fi",
		`if [ -z "$active_slot" ] && docker ps --format '{{.Names}}' | grep -Fx -- "${APP_CONTAINER_BASE}-b" >/dev/null 2>&1; then`,
		`  active_slot='b'`,
		"fi",
		`if [ -z "$active_slot" ] && docker ps --format '{{.Names}}' | grep -Fx -- "$legacy_container" >/dev/null 2>&1; then`,
		`  active_slot='a'`,
		"fi",
		"if [ \"$active_slot\" = 'a' ]; then",
		"  next_slot='b'",
		"  next_port=$SECONDARY_PORT",
		"  old_slot='a'",
		"else",
		"  next_slot='a'",
		"  next_port=$PRIMARY_PORT",
		"  old_slot='b'",
		"fi",
		"next_container=\"$APP_CONTAINER_BASE-$next_slot\"",
		"old_container=\"$APP_CONTAINER_BASE-$old_slot\"",
		"HEALTHCHECK_PATH="+shellQuote(healthPath),
		fmt.Sprintf("cd %s", shellQuote(releaseDir)),
		"docker build -t \"$RELEASE_IMAGE\" -f Dockerfile .",
		"docker rm -f \"$next_container\" >/dev/null 2>&1 || true",
		fmt.Sprintf("docker run -d --restart unless-stopped --network %s --env-file %s --name \"$next_container\" -p 127.0.0.1:${next_port}:%d%s \"$RELEASE_IMAGE\" >/dev/null",
			shellQuote(sharedDockerNetwork), shellQuote(runtimeEnvPath), opts.ContainerPort, renderVolumeFlags(opts.Volumes)),
	)
	if opts.PostDeploy != nil && strings.TrimSpace(opts.PostDeploy.Command) != "" {
		lines = append(lines,
			fmt.Sprintf("if ! docker exec \"$next_container\" bash -lc %s; then", shellQuote(opts.PostDeploy.Command)),
			"  docker logs \"$next_container\" || true",
			"  exit 1",
			"fi",
		)
	}
	lines = append(lines,
		"attempt=0",
		"until docker run --rm --network host curlimages/curl:8.12.1 -fsS \"http://127.0.0.1:${next_port}${HEALTHCHECK_PATH}\" >/dev/null 2>&1; do",
		"  attempt=$((attempt + 1))",
		"  if [ \"$attempt\" -ge 30 ]; then",
		"    docker logs \"$next_container\" || true",
		"    exit 1",
		"  fi",
		"  sleep 2",
		"done",
		"TARGET_PORT=\"$next_port\"",
	)
	if len(opts.Routes) > 0 {
		lines = append(lines, renderManagedRouteConfigCommands(opts, "127.0.0.1:${TARGET_PORT}")...)
	} else {
		lines = append(lines, renderMaintenanceAwareSiteConfigCommands(opts, siteConfigPath, nextSiteCaddy)...)
	}
	lines = append(lines, renderProxyReloadCommands(opts, rootCaddyPath, proxySitesDir, proxyDataPath, proxyConfigPath)...)
	lines = append(lines,
		"if docker ps --format '{{.Names}}' | grep -Fx -- \"$old_container\" >/dev/null 2>&1; then",
		"  docker rm -f \"$old_container\" >/dev/null 2>&1 || true",
		"fi",
		"if docker ps --format '{{.Names}}' | grep -Fx -- \"$legacy_container\" >/dev/null 2>&1; then",
		"  docker rm -f \"$legacy_container\" >/dev/null 2>&1 || true",
		"fi",
		"printf '%s\\n' \"$next_slot\" > \"$ACTIVE_SLOT_FILE\"",
		fmt.Sprintf("printf '%%s\\n' %s > \"$CURRENT_RELEASE_FILE\"", shellQuote(opts.ReleaseID)),
		fmt.Sprintf("printf '%%s\\t%%s\\t%%s\\t%%s\\t%%s\\t%%s\\n' %s \"$next_slot\" \"$next_port\" \"$RELEASE_IMAGE\" %s \"$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)\" >> \"$HISTORY_FILE\"",
			shellQuote(opts.ReleaseID),
			shellQuote(opts.ArtifactSHA)),
		fmt.Sprintf("history_limit=%d", releaseKeepCount(opts)),
		"if [ -f \"$HISTORY_FILE\" ]; then tail -n \"$history_limit\" \"$HISTORY_FILE\" > \"$HISTORY_FILE.tmp\" && mv \"$HISTORY_FILE.tmp\" \"$HISTORY_FILE\"; fi",
		cleanup,
		renderHostCleanupRunCommand(),
	)
	return strings.Join(lines, "\n")
}

func renderStaticHetznerDeployScript(opts RemoteOptions) string {
	if strings.TrimSpace(opts.ReleaseID) == "" {
		opts.ReleaseID = "release"
	}
	if strings.TrimSpace(opts.ArtifactSHA) == "" {
		opts.ArtifactSHA = "unknown"
	}
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	proxySitesDir := filepath.ToSlash(filepath.Join(proxyRoot, "sites"))
	rootCaddyPath := filepath.ToSlash(filepath.Join(proxyRoot, "Caddyfile"))
	proxyDataPath := filepath.ToSlash(filepath.Join(proxyRoot, "data"))
	proxyConfigPath := filepath.ToSlash(filepath.Join(proxyRoot, "config"))
	siteConfigPath := filepath.ToSlash(filepath.Join(proxySitesDir, opts.SiteConfigName))
	releaseDir := releaseDirPath(opts, opts.ReleaseID)
	releasesPath := releasesRootPath(opts)
	historyPath := releaseHistoryPath(opts)
	currentReleaseFile := currentReleasePath(opts)
	staticRootPath := staticCurrentRootPath(opts)
	releaseStaticRoot := staticReleaseRootPath(opts, opts.ReleaseID)
	siteCaddy := renderStaticSiteCaddyfile(opts.Hostnames, opts.RoutePath, staticRootPath, opts.AnalyticsLogName)
	cleanup := renderReleaseCleanupCommands(opts, "history_limit")
	metadataPath := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "analytics-project.json"))
	metadataContents := renderAnalyticsProjectMetadata(opts)
	siteConfigMarker := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, ".site-config"))

	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("cd %s", shellQuote(opts.RemoteAppPath)),
		"if ! command -v docker >/dev/null 2>&1; then",
		"  echo 'docker is required on the remote host' >&2",
		"  exit 1",
		"fi",
		"if ! command -v tar >/dev/null 2>&1; then",
		"  echo 'tar is required on the remote host' >&2",
		"  exit 1",
		"fi",
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(rootCaddyPath), renderRootCaddyfile()),
		fmt.Sprintf("if [ -f %s ]; then", shellQuote(siteConfigMarker)),
		fmt.Sprintf("  prev_site_config=\"$(tr -d '\\r\\n' < %s)\"", shellQuote(siteConfigMarker)),
		fmt.Sprintf("  if [ -n \"$prev_site_config\" ] && [ \"$prev_site_config\" != %s ]; then", shellQuote(opts.SiteConfigName)),
		fmt.Sprintf("    rm -f %s/\"$prev_site_config\"", shellQuote(proxySitesDir)),
		"  fi",
		"fi",
		fmt.Sprintf("printf '%%s\\n' %s > %s", shellQuote(opts.SiteConfigName), shellQuote(siteConfigMarker)),
		fmt.Sprintf("mkdir -p %s", shellQuote(releasesPath)),
		fmt.Sprintf("mkdir -p %s", shellQuote(filepath.Dir(metadataPath))),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(metadataPath), metadataContents),
		fmt.Sprintf("rm -rf %s", shellQuote(releaseDir)),
		fmt.Sprintf("mkdir -p %s", shellQuote(releaseDir)),
		fmt.Sprintf("cp artifact.tar.gz %s", shellQuote(filepath.ToSlash(filepath.Join(releaseDir, "artifact.tar.gz")))),
		fmt.Sprintf("tar -xzf %s -C %s", shellQuote(filepath.ToSlash(filepath.Join(releaseDir, "artifact.tar.gz"))), shellQuote(releaseDir)),
		fmt.Sprintf("if [ ! -d %s ]; then", shellQuote(releaseStaticRoot)),
		fmt.Sprintf("  echo 'static output folder not found after extraction: %s' >&2", releaseStaticRoot),
		"  exit 1",
		"fi",
		fmt.Sprintf("rm -rf %s", shellQuote(staticRootPath)),
		fmt.Sprintf("ln -sfn %s %s", shellQuote(releaseStaticRoot), shellQuote(staticRootPath)),
	}
	lines = append(lines, renderMaintenanceAwareSiteConfigCommands(opts, siteConfigPath, siteCaddy)...)
	lines = append(lines, renderProxyReloadCommands(opts, rootCaddyPath, proxySitesDir, proxyDataPath, proxyConfigPath)...)
	lines = append(lines,
		fmt.Sprintf("HISTORY_FILE=%s", shellQuote(historyPath)),
		fmt.Sprintf("printf '%%s\\n' %s > %s", shellQuote(opts.ReleaseID), shellQuote(currentReleaseFile)),
		fmt.Sprintf("printf '%%s\\t%%s\\t%%s\\t%%s\\t%%s\\t%%s\\n' %s 'static' '0' %s %s \"$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)\" >> \"$HISTORY_FILE\"",
			shellQuote(opts.ReleaseID),
			shellQuote(staticReleaseMarker(opts.ReleaseID)),
			shellQuote(opts.ArtifactSHA)),
		fmt.Sprintf("history_limit=%d", releaseKeepCount(opts)),
		"if [ -f \"$HISTORY_FILE\" ]; then tail -n \"$history_limit\" \"$HISTORY_FILE\" > \"$HISTORY_FILE.tmp\" && mv \"$HISTORY_FILE.tmp\" \"$HISTORY_FILE\"; fi",
		cleanup,
		renderHostCleanupRunCommand(),
	)
	return strings.Join(lines, "\n")
}

func runRemoteScript(opts RemoteOptions, script string) error {
	args := buildSSHArgs(opts, false)
	args = append(args, fmt.Sprintf("%s@%s", opts.RemoteUser, opts.RemoteHost), "bash -se")

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func renderSharedDatabaseSetup(opts HetznerOptions, runtimeEnvPath string) []string {
	postgresRoot := sharedPostgresRoot(opts.RemoteServerPath)
	postgresDataPath := filepath.ToSlash(filepath.Join(postgresRoot, "data"))
	postgresEnvPath := filepath.ToSlash(filepath.Join(postgresRoot, "postgres.env"))
	appDBEnvPath := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, ".database.env"))
	providedPassword := strings.TrimSpace(opts.SharedDatabase.Password)
	passwordAssignment := "APP_DB_PASSWORD=''"
	if providedPassword != "" {
		passwordAssignment = "APP_DB_PASSWORD=" + shellQuote(providedPassword)
	}

	sqlScript := renderSharedDatabaseSQL(*opts.SharedDatabase)

	return []string{
		fmt.Sprintf("mkdir -p %s", shellQuote(postgresRoot)),
		fmt.Sprintf("mkdir -p %s", shellQuote(postgresDataPath)),
		fmt.Sprintf("if [ ! -f %s ]; then", shellQuote(postgresEnvPath)),
		"  POSTGRES_PASSWORD=$(od -An -tx1 -N24 /dev/urandom | tr -d ' \\n')",
		"  POSTGRES_PASSWORD=${POSTGRES_PASSWORD:0:40}",
		fmt.Sprintf("  umask 077 && printf 'POSTGRES_PASSWORD=%%s\\n' \"$POSTGRES_PASSWORD\" > %s", shellQuote(postgresEnvPath)),
		"fi",
		fmt.Sprintf("docker pull %s >/dev/null", shellQuote(sharedPostgresImage(*opts.SharedDatabase))),
		fmt.Sprintf("if ! docker ps -a --format '{{.Names}}' | grep -Fx -- %s >/dev/null 2>&1; then", shellQuote(sharedPostgresContainer)),
		fmt.Sprintf("  docker run -d --restart unless-stopped --network %s -p 127.0.0.1:5432:5432 --name %s --env-file %s -v %s:/var/lib/postgresql/data %s >/dev/null",
			shellQuote(sharedDockerNetwork),
			shellQuote(sharedPostgresContainer),
			shellQuote(postgresEnvPath),
			shellQuote(postgresDataPath),
			shellQuote(sharedPostgresImage(*opts.SharedDatabase))),
		"else",
		fmt.Sprintf("  docker start %s >/dev/null 2>&1 || true", shellQuote(sharedPostgresContainer)),
		"fi",
		fmt.Sprintf("docker network connect %s %s >/dev/null 2>&1 || true",
			shellQuote(sharedDockerNetwork),
			shellQuote(sharedPostgresContainer)),
		"db_attempt=0",
		fmt.Sprintf("until docker exec %s pg_isready -U postgres >/dev/null 2>&1; do", shellQuote(sharedPostgresContainer)),
		"  db_attempt=$((db_attempt + 1))",
		"  if [ \"$db_attempt\" -ge 30 ]; then",
		fmt.Sprintf("    docker logs %s || true", shellQuote(sharedPostgresContainer)),
		"    exit 1",
		"  fi",
		"  sleep 2",
		"done",
		passwordAssignment,
		fmt.Sprintf("if [ -f %s ]; then", shellQuote(appDBEnvPath)),
		fmt.Sprintf("  . %s", shellQuote(appDBEnvPath)),
		"  if [ -z \"$APP_DB_PASSWORD\" ] && [ -n \"${DB_PASSWORD:-}\" ]; then APP_DB_PASSWORD=\"$DB_PASSWORD\"; fi",
		"fi",
		"if [ -z \"$APP_DB_PASSWORD\" ]; then APP_DB_PASSWORD=$(od -An -tx1 -N20 /dev/urandom | tr -d ' \\n'); APP_DB_PASSWORD=${APP_DB_PASSWORD:0:32}; fi",
		fmt.Sprintf("umask 077 && cat > %s <<EOF\nDB_NAME=%s\nDB_USER=%s\nDB_PASSWORD=$APP_DB_PASSWORD\nEOF",
			shellQuote(appDBEnvPath),
			opts.SharedDatabase.Name,
			opts.SharedDatabase.User),
		fmt.Sprintf("chmod 600 %s", shellQuote(appDBEnvPath)),
		fmt.Sprintf("docker exec -i --user postgres %s psql -v ON_ERROR_STOP=1 -v db_password=\"$APP_DB_PASSWORD\" -d postgres <<'SQL'\n%s\nSQL",
			shellQuote(sharedPostgresContainer),
			sqlScript),
		fmt.Sprintf("if grep -q '^DATABASE_URL=' %s; then sed -i '/^DATABASE_URL=/d' %s; fi",
			shellQuote(runtimeEnvPath),
			shellQuote(runtimeEnvPath)),
		fmt.Sprintf("printf 'DATABASE_URL=postgresql://%s:%%s@%s:5432/%s?sslmode=disable\\n' \"$APP_DB_PASSWORD\" >> %s",
			opts.SharedDatabase.User,
			sharedPostgresContainer,
			opts.SharedDatabase.Name,
			shellQuote(runtimeEnvPath)),
	}
}

func renderSharedDatabaseSQL(opts SharedDatabaseOptions) string {
	dbNameLiteral := sqlQuoteLiteral(opts.Name)
	dbUserLiteral := sqlQuoteLiteral(opts.User)
	return strings.Join([]string{
		fmt.Sprintf("SELECT set_config('eu_deploy.app_password', %s, false);", ":'db_password'"),
		"DO $$",
		"BEGIN",
		fmt.Sprintf("  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = %s) THEN", dbUserLiteral),
		fmt.Sprintf("    EXECUTE 'CREATE ROLE %s LOGIN PASSWORD ' || quote_literal(current_setting('eu_deploy.app_password'));", sqlQuoteIdentifier(opts.User)),
		"  ELSE",
		fmt.Sprintf("    EXECUTE 'ALTER ROLE %s WITH LOGIN PASSWORD ' || quote_literal(current_setting('eu_deploy.app_password'));", sqlQuoteIdentifier(opts.User)),
		"  END IF;",
		"END",
		"$$;",
		fmt.Sprintf("SELECT 'CREATE DATABASE %s OWNER %s' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = %s)\\gexec",
			sqlQuoteIdentifier(opts.Name),
			sqlQuoteIdentifier(opts.User),
			dbNameLiteral),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s;", sqlQuoteIdentifier(opts.Name), sqlQuoteIdentifier(opts.User)),
	}, "\n")
}

func renderSiteCaddyfile(hostnames []string, routePath string, servicePort int, analyticsLogName string) string {
	return renderSiteCaddyfileWithUpstream(hostnames, routePath, fmt.Sprintf("127.0.0.1:%d", servicePort), analyticsLogName)
}

func renderStaticSiteCaddyfile(hostnames []string, routePath, rootPath, analyticsLogName string) string {
	routePath = normalizeRoutePath(routePath)
	rootPath = strings.TrimSpace(rootPath)

	return renderCaddySiteBlocks(hostnames, func(hostLabel string) []string {
		lines := []string{
			fmt.Sprintf("%s {", hostLabel),
			"  encode zstd gzip",
		}
		lines = append(lines, renderAnalyticsLogBlock(analyticsLogName)...)

		if routePath == "/" {
			lines = append(lines,
				fmt.Sprintf("  root * %s", rootPath),
				"  try_files {path} /index.html",
				"  file_server",
			)
		} else {
			lines = append(lines,
				fmt.Sprintf("  handle_path %s* {", routePath),
				fmt.Sprintf("    root * %s", rootPath),
				"    try_files {path} /index.html",
				"    file_server",
				"  }",
				"  respond 404",
			)
		}
		lines = append(lines, "}")
		return lines
	})
}

func renderSiteCaddyfileWithUpstream(hostnames []string, routePath, upstream, analyticsLogName string) string {
	routePath = normalizeRoutePath(routePath)
	upstream = strings.TrimSpace(upstream)

	return renderCaddySiteBlocks(hostnames, func(hostLabel string) []string {
		lines := []string{
			fmt.Sprintf("%s {", hostLabel),
			"  encode zstd gzip",
		}
		lines = append(lines, renderAnalyticsLogBlock(analyticsLogName)...)

		if routePath == "/" {
			lines = append(lines, fmt.Sprintf("  reverse_proxy %s", upstream))
		} else {
			lines = append(lines,
				fmt.Sprintf("  handle_path %s* {", routePath),
				fmt.Sprintf("    reverse_proxy %s", upstream),
				"  }",
				"  respond 404",
			)
		}
		lines = append(lines, "}")
		return lines
	})
}

func renderAnalyticsLogBlock(logName string) []string {
	path := analyticsLogPath(logName)
	return []string{
		"  log {",
		fmt.Sprintf("    output file %s {", path),
		"      roll_size 50MiB",
		"      roll_keep 5",
		"    }",
		"    format json",
		"  }",
	}
}

func renderAnalyticsProjectMetadata(opts RemoteOptions) string {
	projectName := strings.TrimSpace(opts.ProjectName)
	if projectName == "" {
		projectName = filepath.Base(opts.RemoteAppPath)
	}

	logPath := analyticsLogPath(opts.AnalyticsLogName)
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString(fmt.Sprintf("  \"projectName\": %q,\n", projectName))
	b.WriteString("  \"hostnames\": [")
	for index, hostname := range opts.Hostnames {
		if index > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%q", hostname))
	}
	b.WriteString("],\n")
	b.WriteString(fmt.Sprintf("  \"logPath\": %q\n", logPath))
	b.WriteString("}\n")
	return b.String()
}

func formatCaddySiteHosts(hostnames []string) string {
	seen := map[string]struct{}{}
	ordered := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		if _, ok := seen[hostname]; ok {
			continue
		}
		seen[hostname] = struct{}{}
		ordered = append(ordered, hostname)
	}
	if len(ordered) == 0 {
		return ""
	}
	return strings.Join(ordered, ", ")
}

func renderCaddySiteBlocks(hostnames []string, render func(hostLabel string) []string) string {
	blocks := make([]string, 0, 2)

	for _, hostGroup := range splitCaddySiteHosts(hostnames) {
		hostLabel := formatCaddySiteHosts(hostGroup)
		if hostLabel == "" {
			continue
		}
		blocks = append(blocks, strings.Join(render(hostLabel), "\n"))
	}

	if len(blocks) == 0 {
		return ""
	}

	return strings.Join(blocks, "\n\n") + "\n"
}

func splitCaddySiteHosts(hostnames []string) [][]string {
	httpsHosts := make([]string, 0, len(hostnames))
	httpHosts := make([]string, 0, len(hostnames))

	for _, hostname := range hostnames {
		trimmed := strings.TrimSpace(hostname)
		if trimmed == "" {
			continue
		}

		if isIPAddressHost(trimmed) {
			httpHosts = append(httpHosts, "http://"+trimmed)
			continue
		}

		httpsHosts = append(httpsHosts, trimmed)
	}

	groups := make([][]string, 0, 2)
	if len(httpsHosts) > 0 {
		groups = append(groups, httpsHosts)
	}
	if len(httpHosts) > 0 {
		groups = append(groups, httpHosts)
	}

	return groups
}

func isIPAddressHost(hostname string) bool {
	return net.ParseIP(strings.TrimSpace(hostname)) != nil
}

var cloudflareTrustedProxyRanges = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
}

func renderRootCaddyfile() string {
	lines := []string{
		"{",
		"  servers {",
		"    trusted_proxies static " + strings.Join(cloudflareTrustedProxyRanges, " "),
		"    client_ip_headers CF-Connecting-IP",
		"    trusted_proxies_strict",
		"  }",
		"}",
		"",
		"import /etc/caddy/sites/*.caddy",
		"",
	}

	return strings.Join(lines, "\n")
}

func renderReleaseCleanupCommands(opts RemoteOptions, historyLimitVar string) string {
	releasesPath := releasesRootPath(opts)
	historyPath := releaseHistoryPath(opts)
	imageRepo := releaseImageRepository(opts.ImageTag)

	lines := []string{
		"keep_ids=''",
		fmt.Sprintf("if [ -f %s ]; then", shellQuote(historyPath)),
		fmt.Sprintf("  keep_ids=\"$(awk -F '\\t' '{print $1}' %s | tr '\\n' ' ')\"", shellQuote(historyPath)),
		"fi",
		fmt.Sprintf("if [ -d %s ]; then", shellQuote(releasesPath)),
		fmt.Sprintf("  find %s -mindepth 1 -maxdepth 1 -type d | while read -r dir; do", shellQuote(releasesPath)),
		"    id=\"$(basename \"$dir\")\"",
		"    case \" $keep_ids \" in",
		"      *\" $id \"*) ;;",
		"      *) rm -rf \"$dir\" ;;",
		"    esac",
		"  done",
		"fi",
		fmt.Sprintf("docker images --format '{{.Repository}}:{{.Tag}}' | grep -F %s | while read -r image; do", shellQuote(imageRepo+":")),
		"  tag=\"${image##*:}\"",
		"  case \" $keep_ids latest remote \" in",
		"    *\" $tag \"*) ;;",
		"    *) docker image rm -f \"$image\" >/dev/null 2>&1 || true ;;",
		"  esac",
		"done || true",
	}
	return strings.Join(lines, "\n")
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
	localFallbacks, err := LoadLocalDeployEnvFiles(workDir)
	if err != nil {
		return nil, false, err
	}

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
			value = strings.TrimSpace(localFallbacks[key])
		}
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
		if generatedEnvKey(cfg, key) {
			continue
		}
		value := strings.TrimSpace(cfg.Env.Secret[key])
		if value == "" {
			value = strings.TrimSpace(localFallbacks[key])
		}
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

func prepareDatabaseConfig(cfg *config.Config, workDir string, p *linePrompter) (bool, error) {
	changed := false

	envTemplateKeys, err := parseEnvTemplateKeys(filepath.Join(workDir, ".env.example"))
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	needsDatabase := hasEnvKey(envTemplateKeys, "DATABASE_URL")
	if _, ok := cfg.Env.Secret["DATABASE_URL"]; ok {
		needsDatabase = true
	}
	if _, ok := cfg.Env.Public["DATABASE_URL"]; ok {
		needsDatabase = true
	}

	if cfg.Database == nil && needsDatabase {
		useShared, err := p.Bool("Found DATABASE_URL. Use shared PostgreSQL on this server", false)
		if err != nil {
			return false, err
		}
		if useShared {
			cfg.Database = &config.DatabaseSpec{
				Mode:   "shared",
				Shared: &config.SharedDatabaseSpec{},
			}
			changed = true
		}
	}

	if !usesSharedDatabase(cfg.Database) {
		return changed, nil
	}

	if cfg.Database.Shared == nil {
		cfg.Database.Shared = &config.SharedDatabaseSpec{}
		changed = true
	}
	if strings.TrimSpace(cfg.Database.Shared.Engine) == "" {
		cfg.Database.Shared.Engine = "postgres"
		changed = true
	}
	if strings.TrimSpace(cfg.Database.Shared.Version) == "" {
		value, err := p.String("Shared PostgreSQL version", defaultSharedDatabaseVersion(), true)
		if err != nil {
			return false, err
		}
		cfg.Database.Shared.Version = value
		changed = true
	}
	if strings.TrimSpace(cfg.Database.Shared.Name) == "" {
		value, err := p.String("Shared PostgreSQL database name", defaultSharedDatabaseName(cfg.Project.Name), true)
		if err != nil {
			return false, err
		}
		cfg.Database.Shared.Name = sanitizePostgresIdentifier(value)
		changed = true
	}
	if strings.TrimSpace(cfg.Database.Shared.User) == "" {
		value, err := p.String("Shared PostgreSQL database user", defaultSharedDatabaseUser(cfg.Project.Name), true)
		if err != nil {
			return false, err
		}
		cfg.Database.Shared.User = sanitizePostgresIdentifier(value)
		changed = true
	}

	return changed, nil
}

func promptSharedDatabasePassword(p *linePrompter, user string) (string, error) {
	for {
		value, err := p.String(fmt.Sprintf("Shared PostgreSQL password for %s (leave blank to generate or reuse on the server)", user), "", false)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return "", nil
		}
		if isURLSafeSecret(value) {
			return value, nil
		}
		if _, err := fmt.Fprintln(p.out, "Use only letters, numbers, hyphen, or underscore for a DB password."); err != nil {
			return "", err
		}
	}
}

func usesSharedDatabase(spec *config.DatabaseSpec) bool {
	return spec != nil && strings.TrimSpace(spec.Mode) == "shared"
}

func generatedEnvKey(cfg *config.Config, key string) bool {
	return usesSharedDatabase(cfg.Database) && key == "DATABASE_URL"
}

func hasEnvKey(keys []string, key string) bool {
	for _, item := range keys {
		if item == key {
			return true
		}
	}
	return false
}

func isURLSafeSecret(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
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

func normalizedHealthcheckPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
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

func defaultRemoteServerPath(user string) string {
	if strings.TrimSpace(user) == "" || strings.TrimSpace(user) == "root" {
		return filepath.ToSlash(filepath.Join("/opt", "eu-deploy"))
	}
	return filepath.ToSlash(filepath.Join("/home", user, "eu-deploy"))
}

func defaultRemoteAppPath(serverPath, projectName string) string {
	safeProject := SanitizeDockerName(projectName)
	if safeProject == "" {
		safeProject = "app"
	}
	root := strings.TrimSpace(serverPath)
	if root == "" {
		root = defaultRemoteServerPath("root")
	}
	return filepath.ToSlash(filepath.Join(root, "apps", safeProject))
}

func effectiveRemoteServerPath(spec config.RemoteProviderSpec, projectName string) string {
	if strings.TrimSpace(spec.ServerPath) != "" {
		return filepath.ToSlash(filepath.Clean(spec.ServerPath))
	}
	if strings.TrimSpace(spec.AppPath) != "" {
		return filepath.ToSlash(filepath.Clean(filepath.Dir(spec.AppPath)))
	}
	return defaultRemoteServerPath(spec.User)
}

func defaultHetznerServerPath(user string) string {
	return defaultRemoteServerPath(user)
}

func defaultHetznerAppPath(serverPath, projectName string) string {
	return defaultRemoteAppPath(serverPath, projectName)
}

func effectiveHetznerServerPath(spec config.HetznerSpec, projectName string) string {
	return effectiveRemoteServerPath(spec.RemoteProviderSpec, projectName)
}

func SharedProxyContainerName() string {
	return sharedProxyContainer
}

func SharedDockerNetworkName() string {
	return sharedDockerNetwork
}

func SharedPostgresContainerName() string {
	return sharedPostgresContainer
}

func BuildHetznerSiteConfigName(hostname string) string {
	name := strings.TrimSpace(hostname)
	if name == "" {
		return "site.caddy"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", " ", "-")
	name = replacer.Replace(name)
	name = strings.Trim(name, "-.")
	if name == "" {
		name = "site"
	}
	return name + ".caddy"
}

func BuildAnalyticsLogName(projectName string) string {
	name := strings.TrimSpace(projectName)
	if name == "" {
		return "site"
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	sanitized := strings.Trim(b.String(), "-_")
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}
	if sanitized == "" {
		sanitized = "site"
	}
	return sanitized
}

func analyticsLogPath(logName string) string {
	return filepath.ToSlash(filepath.Join("/var/log/caddy", BuildAnalyticsLogName(logName)+".access.log"))
}

func sharedProxyRoot(serverPath string) string {
	return filepath.ToSlash(filepath.Join(serverPath, sharedProxyDirName))
}

func sharedPostgresRoot(serverPath string) string {
	return filepath.ToSlash(filepath.Join(serverPath, sharedPostgresDirName))
}

func sharedPostgresImage(opts SharedDatabaseOptions) string {
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = defaultSharedDatabaseVersion()
	}
	return "postgres:" + version
}

func defaultSharedDatabaseVersion() string {
	return "16"
}

func defaultSharedDatabaseName(projectName string) string {
	return sanitizePostgresIdentifier(projectName)
}

func defaultSharedDatabaseUser(projectName string) string {
	return sanitizePostgresIdentifier(projectName)
}

func sanitizePostgresIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "app"
	}

	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	sanitized := b.String()
	sanitized = strings.Trim(sanitized, "_")
	for strings.Contains(sanitized, "__") {
		sanitized = strings.ReplaceAll(sanitized, "__", "_")
	}
	if sanitized == "" {
		sanitized = "app"
	}
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "app_" + sanitized
	}
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
		sanitized = strings.TrimRight(sanitized, "_")
	}
	if sanitized == "" {
		sanitized = "app"
	}
	return sanitized
}

func sqlQuoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func sqlQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
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

func buildSSHArgs(opts HetznerOptions, batchMode bool) []string {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + filepath.ToSlash(filepath.Join(os.TempDir(), "eudeploy-known-hosts")),
	}
	if batchMode {
		args = append(args, "-o", "BatchMode=yes")
	}
	if opts.RemotePort > 0 {
		args = append(args, "-p", strconv.Itoa(opts.RemotePort))
	}
	if strings.TrimSpace(opts.SSHKeyPath) != "" {
		args = append(args, "-i", opts.SSHKeyPath)
	}
	return args
}

func buildSCPArgs(opts HetznerOptions) []string {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + filepath.ToSlash(filepath.Join(os.TempDir(), "eudeploy-known-hosts")),
	}
	if opts.RemotePort > 0 {
		args = append(args, "-P", strconv.Itoa(opts.RemotePort))
	}
	if strings.TrimSpace(opts.SSHKeyPath) != "" {
		args = append(args, "-i", opts.SSHKeyPath)
	}
	return args
}

func renderVolumeFlags(volumes []string) string {
	if len(volumes) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, v := range volumes {
		sb.WriteString(" -v ")
		sb.WriteString(shellQuote(v))
	}
	return sb.String()
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
