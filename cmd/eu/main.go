package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sorisltd/eu-deploy/internal/build"
	"github.com/sorisltd/eu-deploy/internal/config"
	"github.com/sorisltd/eu-deploy/internal/deploy"
	"github.com/sorisltd/eu-deploy/internal/detect"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "eu",
		Short: "eu-deploy: EU-first deploy CLI",
	}

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize eudeploy.yaml for this project",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}

			d := detect.Detect(wd)

			cfg := config.Default()
			cfg.Project = config.ProjectSpec{
				Name:      d.ProjectName,
				Framework: d.Framework,
			}

			// Basic sensible defaults for Node web apps
			cfg.Build = config.BuildSpec{
				Command: d.BuildCommand,
				Output:  d.OutputDir,
			}
			cfg.Runtime = config.RuntimeSpec{
				Type:  "web",
				Port:  3000,
				Start: d.StartCommand,
				Healthcheck: config.HealthcheckSpec{
					Path:     "/",
					Interval: "10s",
					Timeout:  "2s",
				},
			}
			cfg.Routes = []config.RouteSpec{
				{Hostname: "my-app.eu-deploy.dev", Path: "/", Target: "web"},
			}
			cfg.Previews = config.PreviewSpec{
				Enabled:          false,
				HostnameTemplate: "pr-{{pr}}.my-app.eu-deploy.dev",
				TTLHours:         72,
			}

			outPath := "eudeploy.yaml"
			if err := config.WriteYAML(outPath, cfg); err != nil {
				return err
			}

			fmt.Printf("OK Detected framework: %s\n", d.Framework)
			fmt.Printf("OK Created %s\n", outPath)
			for _, warning := range d.Warnings {
				fmt.Printf("NOTE %s\n", warning)
			}
			return nil
		},
	}

	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Build and package the project",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfgPath := filepath.Join(wd, "eudeploy.yaml")
			cfg, err := config.ReadYAML(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("eudeploy.yaml not found: run `eu init` first")
				}
				return err
			}

			res, err := build.BuildProject(cfg, wd)
			if err != nil {
				return err
			}

			projectName := build.ArtifactName(cfg, wd)
			fmt.Printf("OK Built %s\n", projectName)
			fmt.Printf("OK Output: %s\n", res.OutputDir)
			fmt.Printf("OK Artifact: %s\n", res.ArtifactPath)
			fmt.Printf("OK SHA256: %s\n", res.SHA256)
			fmt.Printf("OK Metadata: %s\n", filepath.Join(".eudeploy", "build.json"))
			return nil
		},
	}

	deployCmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy the project locally with Docker or to a Hetzner VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}

			target, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			if strings.TrimSpace(target) == "" {
				target = "docker"
			}

			cfgPath := filepath.Join(wd, "eudeploy.yaml")
			cfg, err := config.ReadYAML(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("eudeploy.yaml not found: run `eu init` first")
				}
				return err
			}
			if runtimeType := strings.TrimSpace(cfg.Runtime.Type); runtimeType != "" && runtimeType != "web" {
				return fmt.Errorf("unsupported runtime.type: %s", runtimeType)
			}

			switch target {
			case "docker":
				return runDockerDeploy(cmd, cfg, wd)
			case "hetzner":
				prepared, err := deploy.PrepareHetznerConfig(&cfg, wd, deploy.PrepareHetznerConfigOptions{
					PromptEnv: true,
				})
				if err != nil {
					return err
				}
				if prepared.Changed {
					if err := config.WriteYAML(cfgPath, cfg); err != nil {
						return err
					}
					fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
				}
				return runHetznerDeploy(cfg, wd, prepared)
			default:
				return fmt.Errorf("unsupported target: %s", target)
			}
		},
	}
	deployCmd.Flags().String("target", "docker", "Deployment target (docker|hetzner)")
	deployCmd.Flags().Int("port", 0, "Host port (default runtime.port)")
	deployCmd.Flags().Bool("detach", false, "Run container in background")
	deployCmd.Flags().String("name", "", "Container name (default: eu-<project>)")

	preflightCmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check whether a Hetzner VM is ready for deployment",
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			if strings.TrimSpace(target) == "" {
				target = "hetzner"
			}
			if target != "hetzner" {
				return fmt.Errorf("unsupported target: %s", target)
			}

			cfg, cfgPath, wd, err := loadConfigFromWorkingDir()
			if err != nil {
				return err
			}
			if runtimeType := strings.TrimSpace(cfg.Runtime.Type); runtimeType != "" && runtimeType != "web" {
				return fmt.Errorf("unsupported runtime.type: %s", runtimeType)
			}

			prepared, err := deploy.PrepareHetznerConfig(&cfg, wd, deploy.PrepareHetznerConfigOptions{})
			if err != nil {
				return err
			}
			if prepared.Changed {
				if err := config.WriteYAML(cfgPath, cfg); err != nil {
					return err
				}
				fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
			}

			return runHetznerPreflight(cfg, wd)
		},
	}
	preflightCmd.Flags().String("target", "hetzner", "Preflight target (hetzner)")

	bootstrapCmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Prepare a Hetzner VM for eu-deploy",
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			if strings.TrimSpace(target) == "" {
				target = "hetzner"
			}
			if target != "hetzner" {
				return fmt.Errorf("unsupported target: %s", target)
			}

			cfg, cfgPath, wd, err := loadConfigFromWorkingDir()
			if err != nil {
				return err
			}
			if runtimeType := strings.TrimSpace(cfg.Runtime.Type); runtimeType != "" && runtimeType != "web" {
				return fmt.Errorf("unsupported runtime.type: %s", runtimeType)
			}

			prepared, err := deploy.PrepareHetznerConfig(&cfg, wd, deploy.PrepareHetznerConfigOptions{})
			if err != nil {
				return err
			}
			if prepared.Changed {
				if err := config.WriteYAML(cfgPath, cfg); err != nil {
					return err
				}
				fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
			}
			if cfg.Hetzner == nil {
				return fmt.Errorf("hetzner config is missing")
			}

			opts, err := deploy.PromptHetznerBootstrapOptions(cfg)
			if err != nil {
				return err
			}

			fmt.Printf("Bootstrapping %s@%s...\n", cfg.Hetzner.User, cfg.Hetzner.Host)
			if err := deploy.BootstrapHetzner(opts); err != nil {
				return err
			}

			fmt.Printf("OK Server root: %s\n", opts.RemoteServerPath)
			fmt.Printf("OK App path: %s\n", opts.RemoteAppPath)
			if cfg.Hetzner.User != "root" {
				fmt.Printf("NOTE Reconnect your SSH session before deploy so docker group membership takes effect for %s.\n", cfg.Hetzner.User)
			}
			return nil
		},
	}
	bootstrapCmd.Flags().String("target", "hetzner", "Bootstrap target (hetzner)")

	rootCmd.AddCommand(initCmd, buildCmd, deployCmd, preflightCmd, bootstrapCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runDockerDeploy(cmd *cobra.Command, cfg config.Config, wd string) error {
	res, built, err := build.EnsureArtifact(cfg, wd, true)
	if err != nil {
		return err
	}
	if built {
		fmt.Printf("OK Build complete\n")
	}

	if strings.TrimSpace(cfg.Runtime.Start) == "" {
		return fmt.Errorf("runtime.start is empty in eudeploy.yaml")
	}
	if cfg.Runtime.Port == 0 {
		return fmt.Errorf("runtime.port is empty in eudeploy.yaml")
	}

	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return err
	}
	if port == 0 {
		port = cfg.Runtime.Port
	}

	nameFlag, err := cmd.Flags().GetString("name")
	if err != nil {
		return err
	}

	projectName := build.ArtifactName(cfg, wd)
	safeProject := deploy.SanitizeDockerName(projectName)

	containerName := strings.TrimSpace(nameFlag)
	if containerName == "" {
		containerName = fmt.Sprintf("eu-%s", safeProject)
	}
	containerName = deploy.SanitizeDockerName(containerName)

	imageTag := fmt.Sprintf("eu-deploy-%s:local", safeProject)

	detach, err := cmd.Flags().GetBool("detach")
	if err != nil {
		return err
	}
	installDependencies, err := build.RequiresDependencyInstall(cfg, wd)
	if err != nil {
		return err
	}

	opts := deploy.DockerOptions{
		WorkDir:             wd,
		ArtifactPath:        res.ArtifactPath,
		RuntimeStart:        cfg.Runtime.Start,
		ContainerPort:       cfg.Runtime.Port,
		HostPort:            port,
		ImageTag:            imageTag,
		ContainerName:       containerName,
		Detach:              detach,
		InstallDependencies: installDependencies,
	}

	if err := deploy.BuildDockerImage(opts); err != nil {
		return err
	}

	fmt.Printf("OK Image: %s\n", imageTag)
	if detach {
		fmt.Printf("Starting container %s in background...\n", containerName)
	} else {
		fmt.Printf("Starting container %s in attached mode on http://localhost:%d\n", containerName, port)
	}

	if err := deploy.RunDockerContainer(opts); err != nil {
		return err
	}
	if detach {
		fmt.Printf("OK Container: %s\n", containerName)
		fmt.Printf("✓ Running at http://localhost:%d\n", port)
	}
	return nil
}

func runHetznerDeploy(cfg config.Config, wd string, prepared deploy.PrepareHetznerResult) error {
	res, built, err := build.EnsureArtifact(cfg, wd, true)
	if err != nil {
		return err
	}
	if built {
		fmt.Printf("OK Build complete\n")
	}

	if strings.TrimSpace(cfg.Runtime.Start) == "" {
		return fmt.Errorf("runtime.start is empty in eudeploy.yaml")
	}
	if cfg.Runtime.Port == 0 {
		return fmt.Errorf("runtime.port is empty in eudeploy.yaml")
	}
	if cfg.Hetzner == nil {
		return fmt.Errorf("hetzner config is missing")
	}
	if len(cfg.Routes) == 0 || strings.TrimSpace(cfg.Routes[0].Hostname) == "" {
		return fmt.Errorf("routes[0].hostname is required for hetzner deploys")
	}

	projectName := build.ArtifactName(cfg, wd)
	safeProject := deploy.SanitizeDockerName(projectName)
	installDependencies, err := build.RequiresDependencyInstall(cfg, wd)
	if err != nil {
		return err
	}

	opts := buildHetznerOptions(cfg, wd, safeProject, prepared.SharedDatabasePassword)
	opts.ArtifactPath = res.ArtifactPath
	opts.InstallDependencies = installDependencies
	opts.Env = prepared.EnvValues

	fmt.Printf("Uploading release to %s@%s...\n", cfg.Hetzner.User, cfg.Hetzner.Host)
	if err := deploy.DeployToHetzner(opts); err != nil {
		return err
	}

	fmt.Printf("OK Server root: %s\n", cfg.Hetzner.ServerPath)
	fmt.Printf("OK Remote app path: %s\n", cfg.Hetzner.AppPath)
	fmt.Printf("✓ Running at https://%s\n", cfg.Routes[0].Hostname)
	return nil
}

func runHetznerPreflight(cfg config.Config, wd string) error {
	projectName := build.ArtifactName(cfg, wd)
	opts := buildHetznerOptions(cfg, wd, deploy.SanitizeDockerName(projectName), "")

	results, err := deploy.PreflightHetzner(opts)
	if err != nil {
		return err
	}

	var failed bool
	for _, result := range results {
		label := "OK"
		switch result.Status {
		case deploy.PreflightWarning:
			label = "WARN"
		case deploy.PreflightFailure:
			label = "FAIL"
			failed = true
		}
		fmt.Printf("%-5s %s: %s\n", label, result.Name, result.Detail)
	}

	if failed {
		return fmt.Errorf("preflight failed")
	}

	if slices.ContainsFunc(results, func(result deploy.PreflightResult) bool {
		return result.Status == deploy.PreflightWarning
	}) {
		fmt.Println("NOTE Preflight passed with warnings.")
		return nil
	}

	fmt.Println("OK Preflight passed.")
	return nil
}

func buildHetznerOptions(cfg config.Config, wd, safeProject, sharedDatabasePassword string) deploy.HetznerOptions {
	opts := deploy.HetznerOptions{
		WorkDir:            wd,
		RuntimeStart:       cfg.Runtime.Start,
		ContainerPort:      cfg.Runtime.Port,
		ServicePort:        cfg.Hetzner.ServicePort,
		ImageTag:           fmt.Sprintf("eu-deploy-%s:remote", safeProject),
		AppContainerName:   fmt.Sprintf("eu-%s-app", safeProject),
		ProxyContainerName: deploy.SharedProxyContainerName(),
		RemoteHost:         cfg.Hetzner.Host,
		RemoteUser:         cfg.Hetzner.User,
		RemotePort:         cfg.Hetzner.Port,
		SSHKeyPath:         cfg.Hetzner.SSHKeyPath,
		RemoteServerPath:   cfg.Hetzner.ServerPath,
		RemoteAppPath:      cfg.Hetzner.AppPath,
		Hostname:           cfg.Routes[0].Hostname,
		RoutePath:          cfg.Routes[0].Path,
		HealthcheckPath:    cfg.Runtime.Healthcheck.Path,
		SiteConfigName:     deploy.BuildHetznerSiteConfigName(cfg.Routes[0].Hostname),
	}

	if cfg.Database != nil && strings.TrimSpace(cfg.Database.Mode) == "shared" && cfg.Database.Shared != nil {
		opts.SharedDatabase = &deploy.SharedDatabaseOptions{
			Version:  cfg.Database.Shared.Version,
			Name:     cfg.Database.Shared.Name,
			User:     cfg.Database.Shared.User,
			Password: sharedDatabasePassword,
		}
	}
	if cfg.Deploy.PostDeploy != nil && strings.TrimSpace(cfg.Deploy.PostDeploy.Command) != "" {
		opts.PostDeploy = &deploy.PostDeployOptions{
			Command: cfg.Deploy.PostDeploy.Command,
			Include: append([]string(nil), cfg.Deploy.PostDeploy.Include...),
		}
	}

	return opts
}

func loadConfigFromWorkingDir() (config.Config, string, string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return config.Config{}, "", "", err
	}

	cfgPath := filepath.Join(wd, "eudeploy.yaml")
	cfg, err := config.ReadYAML(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.Config{}, "", "", fmt.Errorf("eudeploy.yaml not found: run `eu init` first")
		}
		return config.Config{}, "", "", err
	}

	return cfg, cfgPath, wd, nil
}
