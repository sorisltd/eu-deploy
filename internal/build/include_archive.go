package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PackagePaths creates a gzip tarball at destPath containing the selected
// files or directories from workDir, preserving their relative paths.
func PackagePaths(workDir string, includePaths []string, destPath string) error {
	stageDir, err := os.MkdirTemp("", "eudeploy-include-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)

	added := false
	seen := map[string]struct{}{}

	for _, rawPath := range includePaths {
		relPath := strings.TrimSpace(rawPath)
		if relPath == "" {
			continue
		}
		if filepath.IsAbs(relPath) {
			return fmt.Errorf("deploy.post_deploy.include must use relative paths: %s", rawPath)
		}

		relPath = filepath.Clean(relPath)
		if relPath == "." || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			return fmt.Errorf("deploy.post_deploy.include path escapes the project root: %s", rawPath)
		}
		if _, ok := seen[relPath]; ok {
			continue
		}
		seen[relPath] = struct{}{}

		sourcePath := filepath.Join(workDir, relPath)
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return fmt.Errorf("deploy.post_deploy.include path not found: %s", rawPath)
		}

		destPath := filepath.Join(stageDir, relPath)
		switch {
		case info.IsDir():
			if err := copyTree(sourcePath, destPath); err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(sourcePath)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(target, destPath); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(sourcePath, destPath, info.Mode()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported deploy.post_deploy.include path: %s", rawPath)
		}

		added = true
	}

	if !added {
		return nil
	}

	return PackageDirWithRoot(stageDir, destPath, "")
}
