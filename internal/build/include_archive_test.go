package build

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPackagePathsPreservesRelativePaths(t *testing.T) {
	workDir := t.TempDir()
	mustWritePackageFile(t, filepath.Join(workDir, "scripts", "setup-db.js"), "console.log('setup')\n")
	mustWritePackageFile(t, filepath.Join(workDir, "db", "schema.sql"), "create table demo();\n")

	archivePath := filepath.Join(workDir, "include.tar.gz")
	if err := PackagePaths(workDir, []string{"scripts/setup-db.js", "db"}, archivePath); err != nil {
		t.Fatalf("PackagePaths: %v", err)
	}

	entries := readArchiveEntries(t, archivePath)
	for _, expected := range []string{"db/", "db/schema.sql", "scripts/setup-db.js"} {
		if !entries[expected] {
			t.Fatalf("archive missing %s: %v", expected, entries)
		}
	}
}

func TestPackagePathsRejectsEscapingPaths(t *testing.T) {
	workDir := t.TempDir()
	archivePath := filepath.Join(workDir, "include.tar.gz")
	if err := PackagePaths(workDir, []string{"../secret"}, archivePath); err == nil {
		t.Fatalf("expected path escape validation error")
	}
}

func readArchiveEntries(t *testing.T, path string) map[string]bool {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	entries := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("tar.Next: %v", err)
		}
		entries[hdr.Name] = true
	}
	return entries
}
