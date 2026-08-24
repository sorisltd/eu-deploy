package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version  int           `yaml:"version"`
	Project  ProjectSpec   `yaml:"project"`
	Build    BuildSpec     `yaml:"build"`
	Deploy   DeploySpec    `yaml:"deploy,omitempty"`
	Runtime  RuntimeSpec   `yaml:"runtime"`
	Routes   []RouteSpec   `yaml:"routes"`
	Env      EnvSpec       `yaml:"env,omitempty"`
	Previews PreviewSpec   `yaml:"previews,omitempty"`
	Database *DatabaseSpec `yaml:"database,omitempty"`
	Hetzner  *HetznerSpec  `yaml:"hetzner,omitempty"`
	Scaleway *ScalewaySpec `yaml:"scaleway,omitempty"`
	OVH      *OVHSpec      `yaml:"ovh,omitempty"`
}

type ProjectSpec struct {
	Name      string `yaml:"name"`
	Framework string `yaml:"framework"` // auto|nextjs|solidstart|...
}

type BuildSpec struct {
	Command string `yaml:"command"`
	Output  string `yaml:"output"`
}

type DeploySpec struct {
	Provider   string          `yaml:"provider,omitempty"`
	PostDeploy *DeployHookSpec `yaml:"post_deploy,omitempty"`
}

type DeployHookSpec struct {
	Command string   `yaml:"command,omitempty"`
	Include []string `yaml:"include,omitempty"`
}

type HealthcheckSpec struct {
	Path     string `yaml:"path"`
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
}

type RuntimeSpec struct {
	Type            string          `yaml:"type"` // web|static|worker|cron
	Start           string          `yaml:"start,omitempty"`
	Output          string          `yaml:"output,omitempty"`
	Image           string          `yaml:"image,omitempty"`
	NodeVersionFile string          `yaml:"node_version_file,omitempty"`
	Port            int             `yaml:"port,omitempty"`
	Healthcheck     HealthcheckSpec `yaml:"healthcheck,omitempty"`
	Packages        []string        `yaml:"packages,omitempty"`
	Volumes         []string        `yaml:"volumes,omitempty"`
}

var exactNodeVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// RuntimeNodeVersion reads the exact Node version shared by build and runtime
// configuration. An empty node_version_file preserves the previous behavior.
func RuntimeNodeVersion(cfg Config, workDir string) (string, error) {
	versionFile := strings.TrimSpace(cfg.Runtime.NodeVersionFile)
	if versionFile == "" {
		return "", nil
	}
	if !filepath.IsAbs(versionFile) {
		versionFile = filepath.Join(workDir, versionFile)
	}
	raw, err := os.ReadFile(versionFile)
	if err != nil {
		return "", fmt.Errorf("read runtime.node_version_file: %w", err)
	}
	version := strings.TrimSpace(string(raw))
	if !exactNodeVersionPattern.MatchString(version) {
		return "", fmt.Errorf("runtime.node_version_file must contain an exact Node version (for example 22.22.0)")
	}
	return version, nil
}

// RuntimeImage resolves NODE_VERSION in an explicitly configured runtime
// image. The image remains empty when a project uses eu-deploy's default.
func RuntimeImage(cfg Config, workDir string) (string, error) {
	image := strings.TrimSpace(cfg.Runtime.Image)
	if image == "" {
		return "", nil
	}
	if strings.ContainsAny(image, "\r\n\t ") {
		return "", fmt.Errorf("runtime.image must be a single Docker image reference")
	}
	version, err := RuntimeNodeVersion(cfg, workDir)
	if err != nil {
		return "", err
	}
	if strings.Contains(image, "${NODE_VERSION}") {
		if version == "" {
			return "", fmt.Errorf("runtime.image uses ${NODE_VERSION} but runtime.node_version_file is empty")
		}
		image = strings.ReplaceAll(image, "${NODE_VERSION}", version)
	}
	return image, nil
}

type RouteSpec struct {
	Hostname       string   `yaml:"hostname,omitempty"`
	Hostnames      []string `yaml:"hostnames,omitempty"`
	Path           string   `yaml:"path,omitempty"`
	Target         string   `yaml:"target,omitempty"` // web
	PreservePrefix bool     `yaml:"preserve_prefix,omitempty"`
	Redirect       string   `yaml:"redirect,omitempty"`
	Code           int      `yaml:"code,omitempty"`
	CaddyExtraFile string   `yaml:"caddy_extra_file,omitempty"`
}

func ValidateRoutes(routes []RouteSpec) error {
	for index, route := range routes {
		hostCount := 0
		if strings.TrimSpace(route.Hostname) != "" {
			hostCount++
		}
		for _, hostname := range route.Hostnames {
			if strings.TrimSpace(hostname) != "" {
				hostCount++
			}
		}
		if hostCount == 0 {
			return fmt.Errorf("routes[%d] requires hostname or hostnames", index)
		}

		redirect := strings.TrimSpace(route.Redirect)
		if redirect == "" {
			if strings.TrimSpace(route.Target) == "" {
				return fmt.Errorf("routes[%d].target is required for proxy routes", index)
			}
			if route.Code != 0 {
				return fmt.Errorf("routes[%d].code is only valid with redirect", index)
			}
			continue
		}

		if strings.TrimSpace(route.Target) != "" {
			return fmt.Errorf("routes[%d] cannot set both target and redirect", index)
		}
		if route.PreservePrefix {
			return fmt.Errorf("routes[%d].preserve_prefix is only valid for proxy routes", index)
		}
		if strings.TrimSpace(route.CaddyExtraFile) != "" {
			return fmt.Errorf("routes[%d].caddy_extra_file is only valid for proxy routes", index)
		}
		target, err := url.Parse(redirect)
		if err != nil || target.Scheme != "https" || target.Host == "" || !target.IsAbs() {
			return fmt.Errorf("routes[%d].redirect must be an absolute https URL", index)
		}
		if route.Code != 0 && route.Code != 301 && route.Code != 302 && route.Code != 307 && route.Code != 308 {
			return fmt.Errorf("routes[%d].code must be 301, 302, 307, or 308", index)
		}
	}
	return nil
}

type EnvSpec struct {
	Public map[string]string `yaml:"public,omitempty"`
	Secret map[string]string `yaml:"secret,omitempty"`
}

type PreviewSpec struct {
	Enabled          bool   `yaml:"enabled"`
	HostnameTemplate string `yaml:"hostname_template"`
	TTLHours         int    `yaml:"ttl_hours"`
}

type DatabaseSpec struct {
	Mode    string `yaml:"mode"` // external|managed|shared
	Managed *struct {
		Plan   string `yaml:"plan"`
		Region string `yaml:"region"`
		PITR   bool   `yaml:"pitr"`
	} `yaml:"managed,omitempty"`
	Shared *SharedDatabaseSpec `yaml:"shared,omitempty"`
}

type SharedDatabaseSpec struct {
	Engine  string `yaml:"engine,omitempty"`  // postgres
	Version string `yaml:"version,omitempty"` // postgres image tag, e.g. 16
	Name    string `yaml:"name,omitempty"`
	User    string `yaml:"user,omitempty"`
}

type HetznerSpec struct {
	RemoteProviderSpec `yaml:",inline"`
}

type ScalewaySpec struct {
	RemoteProviderSpec `yaml:",inline"`
}

type OVHSpec struct {
	RemoteProviderSpec `yaml:",inline"`
}

type RemoteProviderSpec struct {
	Host        string `yaml:"host"`
	User        string `yaml:"user"`
	Port        int    `yaml:"port,omitempty"`
	SSHKeyPath  string `yaml:"ssh_key_path,omitempty"`
	ServerPath  string `yaml:"server_path,omitempty"`
	AppPath     string `yaml:"app_path,omitempty"`
	ServicePort int    `yaml:"service_port,omitempty"`
}

func Default() Config {
	return Config{
		Version: 1,
		Project: ProjectSpec{
			Name:      "my-app",
			Framework: "auto",
		},
		Build: BuildSpec{
			Command: "npm run build",
			Output:  "dist",
		},
		Runtime: RuntimeSpec{
			Type:  "web",
			Start: "npm run start",
			Port:  3000,
			Healthcheck: HealthcheckSpec{
				Path:     "/",
				Interval: "10s",
				Timeout:  "2s",
			},
		},
		Routes: []RouteSpec{
			{Hostname: "my-app.eu-deploy.dev", Path: "/", Target: "web"},
		},
		Previews: PreviewSpec{
			Enabled:          false,
			HostnameTemplate: "pr-{{pr}}.my-app.eu-deploy.dev",
			TTLHours:         72,
		},
	}
}

func WriteYAML(path string, cfg Config) error {
	b, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	// Write with safe permissions
	return os.WriteFile(path, b, 0o644)
}

func ReadYAML(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func NormalizeRuntimeType(spec RuntimeSpec) string {
	runtimeType := strings.TrimSpace(strings.ToLower(spec.Type))
	if runtimeType == "" {
		return "web"
	}
	return runtimeType
}

func EffectiveBuildOutput(cfg Config) string {
	if output := strings.TrimSpace(cfg.Build.Output); output != "" {
		return output
	}
	if NormalizeRuntimeType(cfg.Runtime) == "static" {
		return strings.TrimSpace(cfg.Runtime.Output)
	}
	return ""
}

func EffectiveStaticArchiveRoot(cfg Config) string {
	output := EffectiveBuildOutput(cfg)
	if output == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(output))
}
