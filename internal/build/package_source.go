package build

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/config"
)

type PackageSource struct {
	SourceDir                 string
	ArchiveRoot               string
	RequiresDependencyInstall bool
	Cleanup                   func() error
}

func ResolvePackageSource(cfg config.Config, workDir string) (PackageSource, error) {
	outputDir := cfg.Build.Output
	outputPath := outputDir
	if !filepath.IsAbs(outputDir) {
		outputPath = filepath.Join(workDir, outputDir)
	}

	info, err := os.Stat(outputPath)
	if err != nil || !info.IsDir() {
		return PackageSource{}, fmt.Errorf("build output folder not found: %s", outputDir)
	}

	if standalone, err := isNextStandaloneOutput(cfg, workDir); err != nil {
		return PackageSource{}, err
	} else if standalone {
		return stageNextStandalone(workDir, outputPath)
	}
	if nextBuild, err := isNextStandardOutput(cfg, workDir); err != nil {
		return PackageSource{}, err
	} else if nextBuild {
		return stageNextStandard(workDir, outputPath)
	}

	return PackageSource{
		SourceDir:                 outputPath,
		ArchiveRoot:               filepath.Base(filepath.Clean(outputPath)),
		RequiresDependencyInstall: true,
		Cleanup:                   func() error { return nil },
	}, nil
}

func RequiresDependencyInstall(cfg config.Config, workDir string) (bool, error) {
	standalone, err := isNextStandaloneOutput(cfg, workDir)
	if err != nil {
		return false, err
	}
	return !standalone, nil
}

func isNextStandaloneOutput(cfg config.Config, workDir string) (bool, error) {
	if strings.TrimSpace(cfg.Project.Framework) != "nextjs" {
		return false, nil
	}

	outputRel, err := outputPathRelativeToWorkDir(cfg.Build.Output, workDir)
	if err != nil {
		return false, err
	}
	return filepath.ToSlash(outputRel) == ".next/standalone", nil
}

func isNextStandardOutput(cfg config.Config, workDir string) (bool, error) {
	if strings.TrimSpace(cfg.Project.Framework) != "nextjs" {
		return false, nil
	}

	outputRel, err := outputPathRelativeToWorkDir(cfg.Build.Output, workDir)
	if err != nil {
		return false, err
	}
	return filepath.ToSlash(outputRel) == ".next", nil
}

func stageNextStandalone(workDir, outputPath string) (PackageSource, error) {
	stageDir, err := os.MkdirTemp("", "eudeploy-next-standalone-*")
	if err != nil {
		return PackageSource{}, err
	}

	cleanup := func() error {
		return os.RemoveAll(stageDir)
	}

	stageOutputPath := filepath.Join(stageDir, ".next", "standalone")
	if err := copyTree(outputPath, stageOutputPath); err != nil {
		cleanup()
		return PackageSource{}, err
	}

	staticPath := filepath.Join(workDir, ".next", "static")
	if info, err := os.Stat(staticPath); err == nil && info.IsDir() {
		if err := copyTree(staticPath, filepath.Join(stageOutputPath, ".next", "static")); err != nil {
			cleanup()
			return PackageSource{}, err
		}
	}

	publicPath := filepath.Join(workDir, "public")
	if info, err := os.Stat(publicPath); err == nil && info.IsDir() {
		if err := copyTree(publicPath, filepath.Join(stageOutputPath, "public")); err != nil {
			cleanup()
			return PackageSource{}, err
		}
	}

	return PackageSource{
		SourceDir:                 stageDir,
		ArchiveRoot:               "",
		RequiresDependencyInstall: false,
		Cleanup:                   cleanup,
	}, nil
}

func stageNextStandard(workDir, outputPath string) (PackageSource, error) {
	stageDir, err := os.MkdirTemp("", "eudeploy-next-standard-*")
	if err != nil {
		return PackageSource{}, err
	}

	cleanup := func() error {
		return os.RemoveAll(stageDir)
	}

	if err := copyTree(outputPath, filepath.Join(stageDir, ".next")); err != nil {
		cleanup()
		return PackageSource{}, err
	}

	publicPath := filepath.Join(workDir, "public")
	if info, err := os.Stat(publicPath); err == nil && info.IsDir() {
		if err := copyTree(publicPath, filepath.Join(stageDir, "public")); err != nil {
			cleanup()
			return PackageSource{}, err
		}
	}

	return PackageSource{
		SourceDir:                 stageDir,
		ArchiveRoot:               "",
		RequiresDependencyInstall: true,
		Cleanup:                   cleanup,
	}, nil
}

func copyTree(srcDir, destDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		destPath := destDir
		if rel != "." {
			destPath = filepath.Join(destDir, rel)
		}

		switch {
		case info.IsDir():
			return os.MkdirAll(destPath, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			return os.Symlink(target, destPath)
		case info.Mode().IsRegular():
			return copyFile(path, destPath, info.Mode())
		default:
			return nil
		}
	})
}

func copyFile(srcPath, destPath string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(destFile, srcFile); err != nil {
		destFile.Close()
		return err
	}
	if err := destFile.Close(); err != nil {
		return err
	}

	return nil
}
