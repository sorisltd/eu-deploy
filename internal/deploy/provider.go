package deploy

import (
	"fmt"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/config"
)

type RemoteTarget string

const (
	RemoteTargetHetzner  RemoteTarget = "hetzner"
	RemoteTargetScaleway RemoteTarget = "scaleway"
	RemoteTargetOVH      RemoteTarget = "ovh"
)

func SupportedRemoteTargets() []string {
	return []string{
		string(RemoteTargetHetzner),
		string(RemoteTargetScaleway),
		string(RemoteTargetOVH),
	}
}

func ParseRemoteTarget(value string) (RemoteTarget, error) {
	switch RemoteTarget(strings.TrimSpace(strings.ToLower(value))) {
	case RemoteTargetHetzner:
		return RemoteTargetHetzner, nil
	case RemoteTargetScaleway:
		return RemoteTargetScaleway, nil
	case RemoteTargetOVH:
		return RemoteTargetOVH, nil
	default:
		return "", fmt.Errorf("unsupported target: %s", value)
	}
}

func IsRemoteTarget(value string) bool {
	_, err := ParseRemoteTarget(value)
	return err == nil
}

func RemoteTargetLabel(target RemoteTarget) string {
	switch target {
	case RemoteTargetScaleway:
		return "Scaleway"
	case RemoteTargetOVH:
		return "OVH"
	default:
		return "Hetzner"
	}
}

func PreferredDeployTarget(cfg config.Config, fallback string) string {
	if value := strings.TrimSpace(strings.ToLower(cfg.Deploy.Provider)); IsRemoteTarget(value) || value == "docker" {
		return value
	}

	configured := make([]string, 0, 3)
	if cfg.Hetzner != nil {
		configured = append(configured, string(RemoteTargetHetzner))
	}
	if cfg.Scaleway != nil {
		configured = append(configured, string(RemoteTargetScaleway))
	}
	if cfg.OVH != nil {
		configured = append(configured, string(RemoteTargetOVH))
	}
	if len(configured) == 1 {
		return configured[0]
	}

	return fallback
}

func EnsureRemoteProviderSpec(cfg *config.Config, target RemoteTarget) *config.RemoteProviderSpec {
	switch target {
	case RemoteTargetScaleway:
		if cfg.Scaleway == nil {
			cfg.Scaleway = &config.ScalewaySpec{}
		}
		return &cfg.Scaleway.RemoteProviderSpec
	case RemoteTargetOVH:
		if cfg.OVH == nil {
			cfg.OVH = &config.OVHSpec{}
		}
		return &cfg.OVH.RemoteProviderSpec
	default:
		if cfg.Hetzner == nil {
			cfg.Hetzner = &config.HetznerSpec{}
		}
		return &cfg.Hetzner.RemoteProviderSpec
	}
}

func RemoteProviderSpecForTarget(cfg config.Config, target RemoteTarget) (*config.RemoteProviderSpec, bool) {
	switch target {
	case RemoteTargetScaleway:
		if cfg.Scaleway == nil {
			return nil, false
		}
		return &cfg.Scaleway.RemoteProviderSpec, true
	case RemoteTargetOVH:
		if cfg.OVH == nil {
			return nil, false
		}
		return &cfg.OVH.RemoteProviderSpec, true
	default:
		if cfg.Hetzner == nil {
			return nil, false
		}
		return &cfg.Hetzner.RemoteProviderSpec, true
	}
}
