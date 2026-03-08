package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/config"
	"github.com/sorisltd/eu-deploy/internal/deploy"
)

func envOverrideNames(target deploy.RemoteTarget, suffix string) []string {
	suffix = strings.TrimSpace(strings.ToUpper(suffix))
	targetName := strings.TrimSpace(strings.ToUpper(string(target)))
	names := []string{}
	if targetName != "" {
		names = append(names, "EUDEPLOY_"+targetName+"_"+suffix)
	}
	names = append(names, "EUDEPLOY_REMOTE_"+suffix)
	return names
}

func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyIntEnv(names ...string) (int, bool) {
	value := firstNonEmptyEnv(names...)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func resolveRemoteProviderSpec(cfg config.Config, target deploy.RemoteTarget) (*config.RemoteProviderSpec, bool) {
	spec, ok := deploy.RemoteProviderSpecForTarget(cfg, target)
	if !ok || spec == nil {
		return nil, false
	}

	resolved := *spec
	if value := firstNonEmptyEnv(envOverrideNames(target, "HOST")...); value != "" {
		resolved.Host = value
	}
	if value := firstNonEmptyEnv(envOverrideNames(target, "USER")...); value != "" {
		resolved.User = value
	}
	if value, ok := firstNonEmptyIntEnv(envOverrideNames(target, "PORT")...); ok {
		resolved.Port = value
	}
	if value := firstNonEmptyEnv(envOverrideNames(target, "SSH_KEY_PATH")...); value != "" {
		resolved.SSHKeyPath = value
	}
	if value := firstNonEmptyEnv(envOverrideNames(target, "SERVER_PATH")...); value != "" {
		resolved.ServerPath = value
	}
	if value := firstNonEmptyEnv(envOverrideNames(target, "APP_PATH")...); value != "" {
		resolved.AppPath = value
	}
	if value, ok := firstNonEmptyIntEnv(envOverrideNames(target, "SERVICE_PORT")...); ok {
		resolved.ServicePort = value
	}

	return &resolved, true
}

func resolveRouteHostname(cfg config.Config) string {
	if value := firstNonEmptyEnv("EUDEPLOY_ROUTE_HOSTNAME", "EUDEPLOY_HOSTNAME"); value != "" {
		return value
	}
	if len(cfg.Routes) == 0 {
		return ""
	}
	return cfg.Routes[0].Hostname
}

func resolveConfigEnvValue(key, configured string) string {
	if value := firstNonEmptyEnv("EUDEPLOY_ENV_"+strings.ToUpper(key), key); value != "" {
		return value
	}
	return strings.TrimSpace(configured)
}

func resolveSharedDatabasePassword(target deploy.RemoteTarget) string {
	names := append([]string{
		"EUDEPLOY_SHARED_DATABASE_PASSWORD",
		"EUDEPLOY_ENV_DATABASE_PASSWORD",
	}, envOverrideNames(target, "SHARED_DATABASE_PASSWORD")...)
	return firstNonEmptyEnv(names...)
}
