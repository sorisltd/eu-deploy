// Package migrations applies plain SQL migration files to a project's Postgres
// database during a remote deploy (and on demand through `eu migrate`).
//
// Files are applied in filename order and recorded in
// public.schema_migrations (filename, sha256, applied_at, how). Everything that
// can be decided locally (file list, checksums, destructive statements, whether
// a file manages its own transaction) is decided here; the remote side gets a
// small bash runner with that plan embedded plus a tarball of the files.
package migrations

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// File is one migration file and what the runner needs to know about it.
type File struct {
	Name        string `json:"name"`
	SHA256      string `json:"sha256"`
	OwnTx       bool   `json:"ownTransaction"`
	Destructive bool   `json:"destructive"`
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.sql$`)

// Plan lists the *.sql files directly in dir, sorted by name (byte order).
func Plan(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("migrations folder %s: %w", dir, err)
	}
	var files []File
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".sql") {
			continue
		}
		if !safeName.MatchString(entry.Name()) {
			return nil, fmt.Errorf("migration file name %q: use only letters, digits, '.', '_' and '-' and end with .sql", entry.Name())
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		text := string(body)
		files = append(files, File{
			Name:        entry.Name(),
			SHA256:      hex.EncodeToString(sum[:]),
			OwnTx:       HasOwnTransaction(text),
			Destructive: IsDestructive(text),
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .sql files in migrations folder %s", dir)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

var (
	allowDestructive = regexp.MustCompile(`(?im)^[ \t]*--[ \t]*allow-destructive\b`)
	lineComment      = regexp.MustCompile(`--[^\n]*`)
	bodyStart        = regexp.MustCompile(`(?is)\bas\s+(\$[A-Za-z_]*\$)`)
	grantRevoke      = regexp.MustCompile(`(?is)\b(grant|revoke)\b[^;]*;`)
	stringLiteral    = regexp.MustCompile(`'[^']*'`)
	whitespace       = regexp.MustCompile(`\s+`)
	destructiveStmt  = regexp.MustCompile(`(?i)\bdrop\s+(table|column|schema|database)\b|\btruncate\b|\bdelete\s+from\b|\balter\s+table[^;]*\bdrop\s+(column\s+)?(if\s+exists\s+)?[a-z_"]+\s*(;|,|cascade|restrict)`)
	ownTx            = regexp.MustCompile(`(?im)^[ \t]*(begin|start\s+transaction)[ \t]*;`)
)

// IsDestructive reports whether the SQL drops or empties data while the
// migration runs: DROP TABLE/COLUMN/SCHEMA/DATABASE, TRUNCATE, DELETE FROM.
// A line "-- allow-destructive" in the file opts out.
//
// Ignored: comments, function bodies (`as $tag$ ... $tag$`, they only run when
// called later), GRANT/REVOKE (privilege names like TRUNCATE) and string
// literals. `do $$ ... $$` blocks are checked, because they run immediately.
func IsDestructive(sql string) bool {
	if allowDestructive.MatchString(sql) {
		return false
	}
	text := lineComment.ReplaceAllString(sql, "")
	text = stripFunctionBodies(text)
	text = grantRevoke.ReplaceAllString(text, "")
	text = stringLiteral.ReplaceAllString(text, "")
	text = whitespace.ReplaceAllString(text, " ")
	return destructiveStmt.MatchString(text)
}

// stripFunctionBodies removes `as $tag$ ... $tag$` (RE2 has no backreferences).
func stripFunctionBodies(text string) string {
	var out strings.Builder
	for {
		loc := bodyStart.FindStringSubmatchIndex(text)
		if loc == nil {
			out.WriteString(text)
			return out.String()
		}
		tag := text[loc[2]:loc[3]]
		end := strings.Index(text[loc[1]:], tag)
		if end < 0 {
			// Unterminated body: drop the rest, like a non-matching regex would leave it.
			out.WriteString(text)
			return out.String()
		}
		out.WriteString(text[:loc[0]])
		out.WriteString(" ")
		text = text[loc[1]+end+len(tag):]
	}
}

// HasOwnTransaction reports whether the file starts its own transaction, in
// which case the runner must not wrap it in another one.
func HasOwnTransaction(sql string) bool {
	return ownTx.MatchString(sql)
}

// PackageArchive writes the planned files (flat, by name) into a .tar.gz.
func PackageArchive(dir string, files []File, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, file := range files {
		body, err := os.ReadFile(filepath.Join(dir, file.Name))
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: file.Name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Close()
}
