package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sorisltd/eu-deploy/internal/config"
	"github.com/sorisltd/eu-deploy/internal/utils"
)

// ArtifactName returns a filesystem-safe name based on config/project or workdir.
func ArtifactName(cfg config.Config, workDir string) string {
	name := strings.TrimSpace(cfg.Project.Name)
	if name == "" {
		name = filepath.Base(workDir)
	}
	return strings.NewReplacer("/", "-", "\\", "-").Replace(name)
}

func ReadMetadata(path string) (Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	var res Result
	if err := json.Unmarshal(b, &res); err != nil {
		return Result{}, err
	}
	return res, nil
}

func EnsureArtifact(cfg config.Config, workDir string, autoBuild bool) (Result, bool, error) {
	currentHash, err := ComputeInputHash(cfg, workDir)
	if err != nil {
		return Result{}, false, err
	}

	metadataPath := filepath.Join(workDir, ".eudeploy", "build.json")
	res, err := ReadMetadata(metadataPath)
	if err == nil && strings.TrimSpace(res.ArtifactPath) != "" {
		artifactPath := res.ArtifactPath
		if !filepath.IsAbs(artifactPath) {
			artifactPath = filepath.Join(workDir, artifactPath)
		}
		if res.InputHash == currentHash {
			if info, statErr := os.Stat(artifactPath); statErr == nil && !info.IsDir() {
				return res, false, nil
			}
		}
	}

	if !autoBuild {
		if strings.TrimSpace(res.ArtifactPath) != "" && res.InputHash != "" && res.InputHash != currentHash {
			return Result{}, false, fmt.Errorf("build artifacts are stale: run `eu build` first")
		}
		return Result{}, false, fmt.Errorf("build artifacts missing: run `eu build` first")
	}

	res, err = BuildProject(cfg, workDir)
	if err != nil {
		return Result{}, false, err
	}
	return res, true, nil
}

func BuildProject(cfg config.Config, workDir string) (Result, error) {
	if strings.TrimSpace(cfg.Build.Command) == "" {
		return Result{}, fmt.Errorf("build.command is empty in eudeploy.yaml")
	}
	if strings.TrimSpace(cfg.Build.Output) == "" {
		return Result{}, fmt.Errorf("build.output is empty in eudeploy.yaml")
	}

	inputHash, err := ComputeInputHash(cfg, workDir)
	if err != nil {
		return Result{}, err
	}

	if err := utils.RunShellCommand(cfg.Build.Command, workDir); err != nil {
		return Result{}, err
	}

	outputDir := cfg.Build.Output
	pkgSource, err := ResolvePackageSource(cfg, workDir)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_ = pkgSource.Cleanup()
	}()

	artifactDir := filepath.Join(workDir, ".eudeploy")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return Result{}, err
	}

	projectName := ArtifactName(cfg, workDir)
	artifactRel := filepath.Join(".eudeploy", fmt.Sprintf("%s.tar.gz", projectName))
	artifactPath := filepath.Join(workDir, artifactRel)

	if err := PackageDirWithRoot(pkgSource.SourceDir, artifactPath, pkgSource.ArchiveRoot); err != nil {
		return Result{}, err
	}

	sha, err := SHA256File(artifactPath)
	if err != nil {
		return Result{}, err
	}

	metadataRel := filepath.Join(".eudeploy", "build.json")
	metadataPath := filepath.Join(workDir, metadataRel)

	res := Result{
		ArtifactPath: artifactRel,
		SHA256:       sha,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		OutputDir:    outputDir,
		InputHash:    inputHash,
	}

	if err := WriteMetadata(metadataPath, res); err != nil {
		return Result{}, err
	}

	return res, nil
}
