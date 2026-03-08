package deploy

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ReleaseRecord struct {
	ID          string `json:"id"`
	Slot        string `json:"slot"`
	Port        int    `json:"port"`
	Image       string `json:"image"`
	ArtifactSHA string `json:"artifactSha"`
	ActivatedAt string `json:"activatedAt"`
}

func BuildReleaseID(sha string) string {
	id := time.Now().UTC().Format("20060102-150405")
	sha = strings.TrimSpace(sha)
	if len(sha) > 12 {
		sha = sha[:12]
	}
	if sha == "" {
		return id
	}
	return id + "-" + sha
}

func releaseHistoryPath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "release-history.tsv"))
}

func activeSlotPath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(opts.RemoteAppPath, ".active-slot"))
}

func currentReleasePath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(opts.RemoteAppPath, ".current-release"))
}

func releasesRootPath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "releases"))
}

func releaseDirPath(opts RemoteOptions, releaseID string) string {
	return filepath.ToSlash(filepath.Join(releasesRootPath(opts), releaseID))
}

func providerConfigFieldPrefix(target RemoteTarget) string {
	if target == "" {
		return string(RemoteTargetHetzner)
	}
	return string(target)
}

func releaseKeepCount(opts RemoteOptions) int {
	if opts.KeepReleases > 0 {
		return opts.KeepReleases
	}
	return defaultReleaseKeepCount
}

func releaseImageRepository(imageTag string) string {
	imageTag = strings.TrimSpace(imageTag)
	lastSlash := strings.LastIndex(imageTag, "/")
	lastColon := strings.LastIndex(imageTag, ":")
	if lastColon > lastSlash {
		return imageTag[:lastColon]
	}
	return imageTag
}

func releaseImageTag(opts RemoteOptions, releaseID string) string {
	repo := releaseImageRepository(opts.ImageTag)
	return repo + ":" + releaseID
}

func releaseSlotContainerName(baseName, slot string) string {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		baseName = "eu-app"
	}
	slot = strings.TrimSpace(slot)
	if slot == "" {
		return baseName
	}
	return baseName + "-" + slot
}

func releaseSlotPorts(basePort int) (int, int) {
	return basePort, basePort + 1
}

func parseReleaseHistory(contents string) ([]ReleaseRecord, error) {
	contents = strings.TrimSpace(contents)
	if contents == "" {
		return nil, nil
	}

	lines := strings.Split(contents, "\n")
	records := make([]ReleaseRecord, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			return nil, fmt.Errorf("invalid release history row: %q", line)
		}
		port, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("invalid release history port in %q: %w", line, err)
		}
		records = append(records, ReleaseRecord{
			ID:          fields[0],
			Slot:        fields[1],
			Port:        port,
			Image:       fields[3],
			ArtifactSHA: fields[4],
			ActivatedAt: fields[5],
		})
	}
	return records, nil
}

func releaseRecordsByNewest(records []ReleaseRecord) []ReleaseRecord {
	out := append([]ReleaseRecord(nil), records...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ActivatedAt > out[j].ActivatedAt
	})
	return out
}

func ReleaseRecordsByNewest(records []ReleaseRecord) []ReleaseRecord {
	return releaseRecordsByNewest(records)
}
