package config

import (
	"os"

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
	Type        string          `yaml:"type"` // web|worker|cron
	Start       string          `yaml:"start"`
	Port        int             `yaml:"port"`
	Healthcheck HealthcheckSpec `yaml:"healthcheck"`
	Packages    []string        `yaml:"packages,omitempty"`
	Volumes     []string        `yaml:"volumes,omitempty"`
}

type RouteSpec struct {
	Hostname string `yaml:"hostname"`
	Path     string `yaml:"path"`
	Target   string `yaml:"target"` // web
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
