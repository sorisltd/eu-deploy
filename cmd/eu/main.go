package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sorisltd/eu-deploy/internal/config"
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

	rootCmd.AddCommand(initCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
