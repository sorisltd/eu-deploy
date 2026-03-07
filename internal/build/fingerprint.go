package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/config"
)

// ComputeInputHash fingerprints the current build inputs so stale artifacts
// can be detected before deploy reuses them.
func ComputeInputHash(cfg config.Config, workDir string) (string, error) {
	h := sha256.New()

	outputRel, err := outputPathRelativeToWorkDir(cfg.Build.Output, workDir)
	if err != nil {
		return "", err
	}

	err = filepath.WalkDir(workDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == workDir {
			return nil
		}

		rel, err := filepath.Rel(workDir, path)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)

		if shouldIgnorePath(rel, d, outputRel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		if _, err := io.WriteString(h, rel); err != nil {
			return err
		}
		if _, err := io.WriteString(h, "\n"); err != nil {
			return err
		}
		if _, err := io.WriteString(h, info.Mode().String()); err != nil {
			return err
		}
		if _, err := io.WriteString(h, "\n"); err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(h, target); err != nil {
				return err
			}
			if _, err := io.WriteString(h, "\n"); err != nil {
				return err
			}
			return nil
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		if _, err := io.WriteString(h, "\n"); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func outputPathRelativeToWorkDir(outputPath, workDir string) (string, error) {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return "", nil
	}

	if filepath.IsAbs(outputPath) {
		rel, err := filepath.Rel(workDir, outputPath)
		if err != nil {
			return "", fmt.Errorf("resolve build.output relative path: %w", err)
		}
		if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", nil
		}
		return filepath.Clean(rel), nil
	}

	return filepath.Clean(outputPath), nil
}

func shouldIgnorePath(rel string, d fs.DirEntry, outputRel string) bool {
	base := filepath.Base(rel)

	if d.IsDir() {
		switch base {
		case ".git", ".eudeploy", "node_modules":
			return true
		}
	}

	if outputRel == "" {
		return false
	}

	if rel == outputRel {
		return true
	}

	return strings.HasPrefix(rel, outputRel+string(os.PathSeparator))
}
