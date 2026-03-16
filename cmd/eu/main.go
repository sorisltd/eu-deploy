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
		Use:           "eu",
		Short:         "eu-deploy: EU-first deploy CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
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

			if commandJSONEnabled(cmd) {
				return emitJSONSuccess(cmd, "", map[string]any{
					"framework": cfg.Project.Framework,
					"path":      outPath,
					"warnings":  d.Warnings,
				})
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
			if commandJSONEnabled(cmd) {
				return emitJSONSuccess(cmd, "", map[string]any{
					"project":   projectName,
					"output":    res.OutputDir,
					"artifact":  res.ArtifactPath,
					"sha256":    res.SHA256,
					"metadata":  filepath.Join(".eudeploy", "build.json"),
					"createdAt": res.CreatedAt,
				})
			}

			fmt.Printf("OK Built %s\n", projectName)
			fmt.Printf("OK Output: %s\n", res.OutputDir)
			fmt.Printf("OK Artifact: %s\n", res.ArtifactPath)
			fmt.Printf("OK SHA256: %s\n", res.SHA256)
			fmt.Printf("OK Metadata: %s\n", filepath.Join(".eudeploy", "build.json"))
			return nil
		},
	}
	addJSONFlag(initCmd)
	addJSONFlag(buildCmd)

	deployCmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy the project locally with Docker or to a remote SSH provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			requestedTarget, err := cmd.Flags().GetString("target")
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
			target := resolveTarget(cfg, requestedTarget, "docker")
			if runtimeType := strings.TrimSpace(cfg.Runtime.Type); runtimeType != "" && runtimeType != "web" {
				return fmt.Errorf("unsupported runtime.type: %s", runtimeType)
			}

			switch target {
			case "docker":
				return runDockerDeploy(cmd, cfg, wd)
			case "hetzner", "scaleway", "ovh":
				remoteTarget, err := deploy.ParseRemoteTarget(target)
				if err != nil {
					return err
				}
				prepared := deploy.PrepareRemoteResult{
					EnvValues:              existingDeployEnvValues(cfg),
					SharedDatabasePassword: resolveSharedDatabasePassword(remoteTarget),
				}
				if commandShouldPrompt(cmd) {
					prepared, err = deploy.PrepareRemoteConfig(&cfg, wd, remoteTarget, deploy.PrepareRemoteConfigOptions{
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
				}
				return runRemoteDeploy(cmd, cfg, wd, remoteTarget, prepared)
			default:
				return fmt.Errorf("unsupported target: %s", target)
			}
		},
	}
	deployCmd.Flags().String("target", "", "Deployment target (docker|hetzner|scaleway|ovh)")
	deployCmd.Flags().Int("port", 0, "Host port (default runtime.port)")
	deployCmd.Flags().Bool("detach", false, "Run container in background")
	deployCmd.Flags().String("name", "", "Container name (default: eu-<project>)")
	addJSONFlag(deployCmd)
	addNoPromptFlag(deployCmd)

	preflightCmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check whether a remote SSH provider VM is ready for deployment",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestedTarget, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			cfg, cfgPath, wd, err := loadConfigFromWorkingDir()
			if err != nil {
				return err
			}
			target := resolveTarget(cfg, requestedTarget, "hetzner")
			if !deploy.IsRemoteTarget(target) {
				return fmt.Errorf("unsupported target: %s", target)
			}
			if runtimeType := strings.TrimSpace(cfg.Runtime.Type); runtimeType != "" && runtimeType != "web" {
				return fmt.Errorf("unsupported runtime.type: %s", runtimeType)
			}

			remoteTarget, err := deploy.ParseRemoteTarget(target)
			if err != nil {
				return err
			}
			if commandShouldPrompt(cmd) {
				prepared, err := deploy.PrepareRemoteConfig(&cfg, wd, remoteTarget, deploy.PrepareRemoteConfigOptions{})
				if err != nil {
					return err
				}
				if prepared.Changed {
					if err := config.WriteYAML(cfgPath, cfg); err != nil {
						return err
					}
					fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
				}
			}

			return runRemotePreflight(cmd, cfg, wd, remoteTarget)
		},
	}
	preflightCmd.Flags().String("target", "", "Preflight target (hetzner|scaleway|ovh)")
	addJSONFlag(preflightCmd)
	addNoPromptFlag(preflightCmd)

	bootstrapCmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Prepare a remote SSH provider VM for eu-deploy",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonMode := commandJSONEnabled(cmd)
			requestedTarget, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}

			cfg, cfgPath, wd, err := loadConfigFromWorkingDir()
			if err != nil {
				return err
			}
			target := resolveTarget(cfg, requestedTarget, "hetzner")
			if !deploy.IsRemoteTarget(target) {
				return fmt.Errorf("unsupported target: %s", target)
			}
			if runtimeType := strings.TrimSpace(cfg.Runtime.Type); runtimeType != "" && runtimeType != "web" {
				return fmt.Errorf("unsupported runtime.type: %s", runtimeType)
			}

			remoteTarget, err := deploy.ParseRemoteTarget(target)
			if err != nil {
				return err
			}
			var opts deploy.HetznerBootstrapOptions
			if jsonMode || commandNoPrompt(cmd) {
				opts, err = buildBootstrapOptions(cfg, remoteTarget)
			} else {
				prepared, err := deploy.PrepareRemoteConfig(&cfg, wd, remoteTarget, deploy.PrepareRemoteConfigOptions{})
				if err != nil {
					return err
				}
				if prepared.Changed {
					if err := config.WriteYAML(cfgPath, cfg); err != nil {
						return err
					}
					fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
				}
				opts, err = deploy.PromptRemoteBootstrapOptions(cfg, remoteTarget)
			}
			if err != nil {
				return err
			}

			if !jsonMode {
				fmt.Printf("Bootstrapping %s@%s...\n", opts.RemoteUser, opts.RemoteHost)
			}
			if err := deploy.BootstrapRemote(opts); err != nil {
				return err
			}

			if jsonMode {
				return emitJSONSuccess(cmd, string(remoteTarget), map[string]any{
					"host":              opts.RemoteHost,
					"user":              opts.RemoteUser,
					"serverRoot":        opts.RemoteServerPath,
					"appPath":           opts.RemoteAppPath,
					"reconnectRequired": opts.RemoteUser != "root",
				})
			}

			fmt.Printf("OK Server root: %s\n", opts.RemoteServerPath)
			fmt.Printf("OK App path: %s\n", opts.RemoteAppPath)
			if opts.RemoteUser != "root" {
				fmt.Printf("NOTE Reconnect your SSH session before deploy so docker group membership takes effect for %s.\n", opts.RemoteUser)
			}
			return nil
		},
	}
	bootstrapCmd.Flags().String("target", "", "Bootstrap target (hetzner|scaleway|ovh)")
	addJSONFlag(bootstrapCmd)
	addNoPromptFlag(bootstrapCmd)

	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream remote container logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonMode := commandJSONEnabled(cmd)
			cfg, cfgPath, wd, err := loadConfigFromWorkingDir()
			if err != nil {
				return err
			}
			requestedTarget, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			target := resolveTarget(cfg, requestedTarget, "hetzner")
			if !deploy.IsRemoteTarget(target) {
				return fmt.Errorf("unsupported target: %s", target)
			}
			remoteTarget, err := deploy.ParseRemoteTarget(target)
			if err != nil {
				return err
			}
			if commandShouldPrompt(cmd) {
				prepared, err := deploy.PrepareRemoteConfig(&cfg, wd, remoteTarget, deploy.PrepareRemoteConfigOptions{})
				if err != nil {
					return err
				}
				if prepared.Changed {
					if err := config.WriteYAML(cfgPath, cfg); err != nil {
						return err
					}
					fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
				}
			}

			component, err := cmd.Flags().GetString("component")
			if err != nil {
				return err
			}
			follow, err := cmd.Flags().GetBool("follow")
			if err != nil {
				return err
			}
			tail, err := cmd.Flags().GetInt("tail")
			if err != nil {
				return err
			}

			opts, err := buildRemoteOptions(cfg, wd, remoteTarget, "", "")
			if err != nil {
				return err
			}
			if jsonMode {
				return deploy.LogsRemoteJSON(opts, component, follow, tail, os.Stdout)
			}
			return deploy.LogsRemote(opts, component, follow, tail)
		},
	}
	logsCmd.Flags().String("target", "", "Logs target (hetzner|scaleway|ovh)")
	logsCmd.Flags().String("component", "app", "Container component to inspect (app|proxy|postgres)")
	logsCmd.Flags().Bool("follow", true, "Follow log output")
	logsCmd.Flags().Int("tail", 200, "Number of log lines to show before streaming")
	addJSONFlag(logsCmd)
	addNoPromptFlag(logsCmd)

	releasesCmd := &cobra.Command{
		Use:   "releases",
		Short: "List remote release history",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonMode := commandJSONEnabled(cmd)
			cfg, cfgPath, wd, err := loadConfigFromWorkingDir()
			if err != nil {
				return err
			}
			requestedTarget, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			target := resolveTarget(cfg, requestedTarget, "hetzner")
			if !deploy.IsRemoteTarget(target) {
				return fmt.Errorf("unsupported target: %s", target)
			}
			remoteTarget, err := deploy.ParseRemoteTarget(target)
			if err != nil {
				return err
			}
			if commandShouldPrompt(cmd) {
				prepared, err := deploy.PrepareRemoteConfig(&cfg, wd, remoteTarget, deploy.PrepareRemoteConfigOptions{})
				if err != nil {
					return err
				}
				if prepared.Changed {
					if err := config.WriteYAML(cfgPath, cfg); err != nil {
						return err
					}
					fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
				}
			}

			opts, err := buildRemoteOptions(cfg, wd, remoteTarget, "", "")
			if err != nil {
				return err
			}
			records, err := deploy.FetchRemoteReleaseHistory(opts)
			if err != nil {
				return err
			}
			views := releaseViewsByNewest(records)
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			if limit > 0 && len(views) > limit {
				views = views[:limit]
			}

			if jsonMode {
				currentRelease := ""
				for _, view := range views {
					if view.Current {
						currentRelease = view.ID
						break
					}
				}
				return emitJSONSuccess(cmd, string(remoteTarget), map[string]any{
					"releases":       views,
					"count":          len(views),
					"totalCount":     len(records),
					"currentRelease": currentRelease,
					"order":          "newestFirst",
					"host":           opts.RemoteHost,
					"appPath":        opts.RemoteAppPath,
				})
			}

			if len(views) == 0 {
				fmt.Println("No releases found.")
				return nil
			}
			for _, view := range views {
				marker := " "
				if view.Current {
					marker = "*"
				}
				fmt.Printf("%s %s  slot=%s port=%d  %s  %s\n", marker, view.ID, view.Slot, view.Port, view.ActivatedAt, view.Image)
			}
			return nil
		},
	}
	releasesCmd.Flags().String("target", "", "Release history target (hetzner|scaleway|ovh)")
	releasesCmd.Flags().Int("limit", 0, "Limit the number of releases shown")
	addJSONFlag(releasesCmd)
	addNoPromptFlag(releasesCmd)

	destroyCmd := &cobra.Command{
		Use:   "destroy",
		Short: "Remove a remote deployment",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonMode := commandJSONEnabled(cmd)
			cfg, cfgPath, wd, err := loadConfigFromWorkingDir()
			if err != nil {
				return err
			}
			requestedTarget, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			target := resolveTarget(cfg, requestedTarget, "hetzner")
			if !deploy.IsRemoteTarget(target) {
				return fmt.Errorf("unsupported target: %s", target)
			}
			remoteTarget, err := deploy.ParseRemoteTarget(target)
			if err != nil {
				return err
			}
			if commandShouldPrompt(cmd) {
				prepared, err := deploy.PrepareRemoteConfig(&cfg, wd, remoteTarget, deploy.PrepareRemoteConfigOptions{})
				if err != nil {
					return err
				}
				if prepared.Changed {
					if err := config.WriteYAML(cfgPath, cfg); err != nil {
						return err
					}
					fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
				}
			}
			opts, err := buildRemoteOptions(cfg, wd, remoteTarget, "", "")
			if err != nil {
				return err
			}
			dropDatabase, err := cmd.Flags().GetBool("drop-database")
			if err != nil {
				return err
			}
			if err := deploy.DestroyRemote(opts, dropDatabase); err != nil {
				return err
			}
			if jsonMode {
				return emitJSONSuccess(cmd, string(remoteTarget), map[string]any{
					"host":         opts.RemoteHost,
					"appPath":      opts.RemoteAppPath,
					"dropDatabase": dropDatabase,
				})
			}
			return nil
		},
	}
	destroyCmd.Flags().String("target", "", "Destroy target (hetzner|scaleway|ovh)")
	destroyCmd.Flags().Bool("drop-database", false, "Also drop the app database and role when using shared PostgreSQL")
	addJSONFlag(destroyCmd)
	addNoPromptFlag(destroyCmd)

	rollbackCmd := &cobra.Command{
		Use:   "rollback",
		Short: "Roll back a remote deployment to the previous release",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonMode := commandJSONEnabled(cmd)
			cfg, cfgPath, wd, err := loadConfigFromWorkingDir()
			if err != nil {
				return err
			}
			requestedTarget, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			target := resolveTarget(cfg, requestedTarget, "hetzner")
			if !deploy.IsRemoteTarget(target) {
				return fmt.Errorf("unsupported target: %s", target)
			}
			remoteTarget, err := deploy.ParseRemoteTarget(target)
			if err != nil {
				return err
			}
			if commandShouldPrompt(cmd) {
				prepared, err := deploy.PrepareRemoteConfig(&cfg, wd, remoteTarget, deploy.PrepareRemoteConfigOptions{})
				if err != nil {
					return err
				}
				if prepared.Changed {
					if err := config.WriteYAML(cfgPath, cfg); err != nil {
						return err
					}
					fmt.Printf("OK Updated %s\n", filepath.Base(cfgPath))
				}
			}
			opts, err := buildRemoteOptions(cfg, wd, remoteTarget, "", "")
			if err != nil {
				return err
			}
			releaseID, err := cmd.Flags().GetString("to")
			if err != nil {
				return err
			}
			record, err := deploy.RollbackRemote(opts, strings.TrimSpace(releaseID))
			if err != nil {
				return err
			}
			if jsonMode {
				return emitJSONSuccess(cmd, string(remoteTarget), map[string]any{
					"release":        jsonReleaseView{ReleaseRecord: record, Current: true},
					"hostname":       cfg.Routes[0].Hostname,
					"databaseNotice": "database schema changes are not rolled back automatically",
				})
			}
			fmt.Printf("OK Rolled back to release: %s\n", record.ID)
			fmt.Printf("OK Image: %s\n", record.Image)
			fmt.Println("NOTE Database schema changes are not rolled back automatically.")
			return nil
		},
	}
	rollbackCmd.Flags().String("target", "", "Rollback target (hetzner|scaleway|ovh)")
	rollbackCmd.Flags().String("to", "", "Specific release ID to activate instead of the previous distinct release")
	addJSONFlag(rollbackCmd)
	addNoPromptFlag(rollbackCmd)

	rootCmd.AddCommand(initCmd, buildCmd, deployCmd, preflightCmd, bootstrapCmd, logsCmd, releasesCmd, destroyCmd, rollbackCmd)

	if err := rootCmd.Execute(); err != nil {
		if argsWantJSON(os.Args[1:]) {
			emitJSONError(err)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func runDockerDeploy(cmd *cobra.Command, cfg config.Config, wd string) error {
	jsonMode := commandJSONEnabled(cmd)
	phases := dockerDeployPhaseDefinitions()
	res, built, err := build.EnsureArtifact(cfg, wd, true)
	if err != nil {
		if jsonMode {
			return newJSONCommandError(cmd, "docker", err, failedPhaseData(phases, 0))
		}
		return err
	}
	if built && !jsonMode {
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
		if jsonMode {
			return newJSONCommandError(cmd, "docker", err, failedPhaseData(phases, 1))
		}
		return err
	}

	if !jsonMode {
		fmt.Printf("OK Image: %s\n", imageTag)
		if detach {
			fmt.Printf("Starting container %s in background...\n", containerName)
		} else {
			fmt.Printf("Starting container %s in attached mode on http://localhost:%d\n", containerName, port)
		}
	}

	if err := deploy.RunDockerContainer(opts); err != nil {
		if jsonMode {
			return newJSONCommandError(cmd, "docker", err, failedPhaseData(phases, 2))
		}
		return err
	}
	if jsonMode {
		return emitJSONSuccess(cmd, "docker", mergeJSONData(
			completedPhaseData(phases),
			map[string]any{
				"built":         built,
				"image":         imageTag,
				"containerName": containerName,
				"hostPort":      port,
				"containerPort": cfg.Runtime.Port,
				"artifact":      res.ArtifactPath,
				"sha256":        res.SHA256,
			},
		))
	}
	if detach {
		fmt.Printf("OK Container: %s\n", containerName)
		fmt.Printf("✓ Running at http://localhost:%d\n", port)
	}
	return nil
}

func runRemoteDeploy(cmd *cobra.Command, cfg config.Config, wd string, target deploy.RemoteTarget, prepared deploy.PrepareRemoteResult) error {
	jsonMode := commandJSONEnabled(cmd)
	phases := remoteDeployPhaseDefinitions()
	currentPhase := 1
	res, built, err := build.EnsureArtifact(cfg, wd, true)
	if err != nil {
		if jsonMode {
			return newJSONCommandError(cmd, string(target), err, failedPhaseData(phases, 0))
		}
		return err
	}
	if built && !jsonMode {
		fmt.Printf("OK Build complete\n")
	}

	if strings.TrimSpace(cfg.Runtime.Start) == "" {
		return fmt.Errorf("runtime.start is empty in eudeploy.yaml")
	}
	if cfg.Runtime.Port == 0 {
		return fmt.Errorf("runtime.port is empty in eudeploy.yaml")
	}
	if len(cfg.Routes) == 0 || strings.TrimSpace(cfg.Routes[0].Hostname) == "" {
		return fmt.Errorf("routes[0].hostname is required for remote deploys")
	}

	installDependencies, err := build.RequiresDependencyInstall(cfg, wd)
	if err != nil {
		return err
	}

	opts, err := buildRemoteOptions(cfg, wd, target, res.SHA256, prepared.SharedDatabasePassword)
	if err != nil {
		return err
	}
	opts.ArtifactPath = res.ArtifactPath
	opts.ArtifactSHA = res.SHA256
	opts.ReleaseID = deploy.BuildReleaseID(res.SHA256)
	opts.InstallDependencies = installDependencies
	opts.Env = prepared.EnvValues

	if !jsonMode {
		fmt.Printf("Uploading release to %s@%s...\n", opts.RemoteUser, opts.RemoteHost)
	}
	if err := deploy.DeployToRemoteWithHooks(opts, deploy.RemoteDeployHooks{
		OnPhase: func(phaseID string) {
			switch phaseID {
			case "activateRelease":
				currentPhase = 2
			default:
				currentPhase = 1
			}
		},
	}); err != nil {
		if jsonMode {
			return newJSONCommandError(cmd, string(target), err, failedPhaseData(phases, currentPhase))
		}
		return err
	}

	if jsonMode {
		return emitJSONSuccess(cmd, string(target), mergeJSONData(
			completedPhaseData(phases),
			map[string]any{
				"built":      built,
				"releaseId":  opts.ReleaseID,
				"hostname":   cfg.Routes[0].Hostname,
				"serverRoot": opts.RemoteServerPath,
				"appPath":    opts.RemoteAppPath,
				"artifact":   res.ArtifactPath,
				"sha256":     res.SHA256,
				"host":       opts.RemoteHost,
				"user":       opts.RemoteUser,
			},
		))
	}

	fmt.Printf("OK Release: %s\n", opts.ReleaseID)
	fmt.Printf("OK Server root: %s\n", opts.RemoteServerPath)
	fmt.Printf("OK Remote app path: %s\n", opts.RemoteAppPath)
	fmt.Printf("✓ Running at https://%s\n", cfg.Routes[0].Hostname)
	return nil
}

func runRemotePreflight(cmd *cobra.Command, cfg config.Config, wd string, target deploy.RemoteTarget) error {
	jsonMode := commandJSONEnabled(cmd)
	opts, err := buildRemoteOptions(cfg, wd, target, "", "")
	if err != nil {
		return err
	}

	results, err := deploy.PreflightRemote(opts)
	if err != nil {
		return err
	}

	var failed bool
	for _, result := range results {
		if jsonMode {
			if result.Status == deploy.PreflightFailure {
				failed = true
			}
			continue
		}
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

	warnings := slices.ContainsFunc(results, func(result deploy.PreflightResult) bool {
		return result.Status == deploy.PreflightWarning
	})

	if jsonMode {
		data := map[string]any{
			"results":       results,
			"hasWarnings":   warnings,
			"failed":        failed,
			"remoteHost":    opts.RemoteHost,
			"remoteUser":    opts.RemoteUser,
			"remoteAppPath": opts.RemoteAppPath,
		}
		if failed {
			return newJSONCommandError(cmd, string(target), fmt.Errorf("preflight failed"), data)
		}
		return emitJSONSuccess(cmd, string(target), data)
	}

	if failed {
		return fmt.Errorf("preflight failed")
	}

	if warnings {
		fmt.Println("NOTE Preflight passed with warnings.")
		return nil
	}

	fmt.Println("OK Preflight passed.")
	return nil
}

func buildRemoteOptions(cfg config.Config, wd string, target deploy.RemoteTarget, artifactSHA, sharedDatabasePassword string) (deploy.RemoteOptions, error) {
	spec, ok := resolveRemoteProviderSpec(cfg, target)
	if !ok || spec == nil {
		return deploy.RemoteOptions{}, fmt.Errorf("%s config is missing", target)
	}

	projectName := build.ArtifactName(cfg, wd)
	safeProject := deploy.SanitizeDockerName(projectName)
	hostnames := resolveRouteHostnames(cfg)
	opts := deploy.RemoteOptions{
		Provider:           target,
		WorkDir:            wd,
		ArtifactSHA:        artifactSHA,
		RuntimeStart:       cfg.Runtime.Start,
		ContainerPort:      cfg.Runtime.Port,
		ServicePort:        spec.ServicePort,
		ImageTag:           fmt.Sprintf("eu-deploy-%s:remote", safeProject),
		AppContainerName:   fmt.Sprintf("eu-%s-app", safeProject),
		ProxyContainerName: deploy.SharedProxyContainerName(),
		RemoteHost:         spec.Host,
		RemoteUser:         spec.User,
		RemotePort:         spec.Port,
		SSHKeyPath:         spec.SSHKeyPath,
		RemoteServerPath:   spec.ServerPath,
		RemoteAppPath:      spec.AppPath,
		Hostname:           resolveRouteHostname(cfg),
		Hostnames:          hostnames,
		RoutePath:          cfg.Routes[0].Path,
		HealthcheckPath:    cfg.Runtime.Healthcheck.Path,
		SiteConfigName:     deploy.BuildHetznerSiteConfigName(cfg.Routes[0].Hostname),
		KeepReleases:       3,
		Packages:           cfg.Runtime.Packages,
		Volumes:            cfg.Runtime.Volumes,
	}
	if len(opts.Hostnames) == 0 && strings.TrimSpace(opts.Hostname) != "" {
		opts.Hostnames = []string{opts.Hostname}
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

	return opts, nil
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

func resolveTarget(cfg config.Config, requested, fallback string) string {
	requested = strings.TrimSpace(strings.ToLower(requested))
	if requested != "" {
		return requested
	}
	return deploy.PreferredDeployTarget(cfg, fallback)
}

func existingDeployEnvValues(cfg config.Config) map[string]string {
	values := map[string]string{}
	generatedDatabaseURL := cfg.Database != nil && strings.TrimSpace(cfg.Database.Mode) == "shared"

	for key, value := range cfg.Env.Public {
		value = resolveConfigEnvValue(key, value)
		if value != "" {
			values[key] = value
		}
	}
	for key, value := range cfg.Env.Secret {
		if generatedDatabaseURL && key == "DATABASE_URL" {
			continue
		}
		value = resolveConfigEnvValue(key, value)
		if value != "" {
			values[key] = value
		}
	}
	return values
}

func buildBootstrapOptions(cfg config.Config, target deploy.RemoteTarget) (deploy.HetznerBootstrapOptions, error) {
	spec, ok := resolveRemoteProviderSpec(cfg, target)
	if !ok || spec == nil {
		return deploy.HetznerBootstrapOptions{}, fmt.Errorf("%s config is missing", target)
	}
	serverPath := strings.TrimSpace(spec.ServerPath)
	if serverPath == "" && strings.TrimSpace(spec.AppPath) != "" {
		serverPath = filepath.ToSlash(filepath.Clean(filepath.Dir(spec.AppPath)))
	}
	if serverPath == "" {
		if strings.TrimSpace(spec.User) == "" || strings.TrimSpace(spec.User) == "root" {
			serverPath = "/opt/eu-deploy"
		} else {
			serverPath = filepath.ToSlash(filepath.Join("/home", spec.User, "eu-deploy"))
		}
	}

	var sharedDatabase *deploy.SharedDatabaseOptions
	if cfg.Database != nil && strings.TrimSpace(cfg.Database.Mode) == "shared" && cfg.Database.Shared != nil {
		sharedDatabase = &deploy.SharedDatabaseOptions{
			Version: cfg.Database.Shared.Version,
			Name:    cfg.Database.Shared.Name,
			User:    cfg.Database.Shared.User,
		}
	}

	return deploy.HetznerBootstrapOptions{
		Provider:         target,
		RemoteHost:       spec.Host,
		RemoteUser:       spec.User,
		RemotePort:       spec.Port,
		SSHKeyPath:       spec.SSHKeyPath,
		RemoteServerPath: serverPath,
		RemoteAppPath:    spec.AppPath,
		InstallUFW:       true,
		InstallFail2ban:  false,
		SharedDatabase:   sharedDatabase,
	}, nil
}
