package deploy

import (
	"path/filepath"
	"strings"
)

func normalizedRuntimeType(value string) string {
	runtimeType := strings.TrimSpace(strings.ToLower(value))
	if runtimeType == "" {
		return "web"
	}
	return runtimeType
}

func isStaticRuntime(value string) bool {
	return normalizedRuntimeType(value) == "static"
}

func staticCurrentRootPath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "static"))
}

func staticReleaseRootPath(opts RemoteOptions, releaseID string) string {
	releaseDir := releaseDirPath(opts, releaseID)
	if strings.TrimSpace(opts.StaticArchiveRoot) == "" {
		return releaseDir
	}
	return filepath.ToSlash(filepath.Join(releaseDir, opts.StaticArchiveRoot))
}

func staticReleaseMarker(releaseID string) string {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return "static"
	}
	return "static:" + releaseID
}
