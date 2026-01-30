package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
					Path:     "/health",
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
		Short: "Deploy the project (local Docker by default)",
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
			if target != "docker" {
				return fmt.Errorf("unsupported target: %s", target)
			}

			cfgPath := filepath.Join(wd, "eudeploy.yaml")
			cfg, err := config.ReadYAML(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("eudeploy.yaml not found: run `eu init` first")
				}
				return err
			}

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

			opts := deploy.DockerOptions{
				WorkDir:       wd,
				ArtifactPath:  res.ArtifactPath,
				RuntimeStart:  cfg.Runtime.Start,
				ContainerPort: cfg.Runtime.Port,
				HostPort:      port,
				ImageTag:      imageTag,
				ContainerName: containerName,
				Detach:        detach,
			}

			if err := deploy.BuildDockerImage(opts); err != nil {
				return err
			}

			fmt.Printf("OK Image: %s\n", imageTag)
			fmt.Printf("OK Container: %s\n", containerName)
			fmt.Printf("✓ Running at http://localhost:%d\n", port)

			if err := deploy.RunDockerContainer(opts); err != nil {
				return err
			}
			return nil
		},
	}
	deployCmd.Flags().String("target", "docker", "Deployment target (docker)")
	deployCmd.Flags().Int("port", 0, "Host port (default runtime.port)")
	deployCmd.Flags().Bool("detach", false, "Run container in background")
	deployCmd.Flags().String("name", "", "Container name (default: eu-<project>)")

	rootCmd.AddCommand(initCmd, buildCmd, deployCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
