package build

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Result struct {
	ArtifactPath string `json:"artifact_path"`
	SHA256       string `json:"sha256"`
	CreatedAt    string `json:"created_at"`
	OutputDir    string `json:"output_dir"`
	InputHash    string `json:"input_hash"`
}

func WriteMetadata(path string, res Result) error {
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// PackageDir creates a gzip tarball at destPath with the contents of srcDir.
// The tarball includes the srcDir as the top-level folder.
func PackageDir(srcDir, destPath string) error {
	return PackageDirWithRoot(srcDir, destPath, filepath.Base(filepath.Clean(srcDir)))
}

// PackageDirWithRoot creates a gzip tarball at destPath with the contents of
// srcDir nested under archiveRoot. If archiveRoot is empty, srcDir contents are
// written at the archive root.
func PackageDirWithRoot(srcDir, destPath, archiveRoot string) error {
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	srcDir = filepath.Clean(srcDir)
	archiveRoot = strings.TrimSpace(filepath.ToSlash(archiveRoot))

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		name := archiveRoot
		if rel != "." {
			if archiveRoot == "" {
				name = filepath.ToSlash(rel)
			} else {
				name = filepath.ToSlash(filepath.Join(archiveRoot, rel))
			}
		} else if archiveRoot == "" {
			return nil
		}
		if info.IsDir() && !strings.HasSuffix(name, "/") {
			name += "/"
		}

		linkname := ""
		if info.Mode()&os.ModeSymlink != 0 {
			linkname, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}

		hdr, err := tar.FileInfoHeader(info, linkname)
		if err != nil {
			return err
		}
		hdr.Name = name

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}

		return nil
	})
}
