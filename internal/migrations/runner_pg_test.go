package migrations

// Runs the generated bash runner against a real Postgres, with a fake `docker`
// on PATH that forwards `docker exec <c> psql|pg_dump` to local binaries.
//
//	EU_MIGRATE_PG="-h /tmp -p 55432 -U postgres" go test ./internal/migrations -run PG -v
//
// Skipped unless EU_MIGRATE_PG is set. Needs bash, psql, pg_dump, sha256sum, tar.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type pgHarness struct {
	t       *testing.T
	conn    string // e.g. "-h /tmp -p 55432 -U postgres"
	db      string
	bin     string
	migDir  string
	runDir  string
	backups string
}

func newPGHarness(t *testing.T, db string) *pgHarness {
	conn := os.Getenv("EU_MIGRATE_PG")
	if conn == "" {
		t.Skip("set EU_MIGRATE_PG to run against a real Postgres")
	}
	h := &pgHarness{t: t, conn: conn, db: db, bin: t.TempDir(), migDir: t.TempDir(), runDir: t.TempDir(), backups: filepath.Join(t.TempDir(), "backups")}
	// Fake docker: `ps` lists the container; `exec [-i] <c> <tool> args...` runs the
	// local tool with the test connection (the runner's -U/-d come after ours and win).
	fake := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  ps) echo testpg ;;
  exec)
    shift; [ "$1" = -i ] && shift
    [ "$1" = testpg ] || { echo "unknown container $1" >&2; exit 1; }
    shift; tool="$1"; shift
    exec "$tool" %s "$@" ;;
  *) echo "fake docker: $*" >&2; exit 1 ;;
esac
`, conn)
	if err := os.WriteFile(filepath.Join(h.bin, "docker"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	h.admin("drop database if exists " + db)
	h.admin("create database " + db)
	t.Cleanup(func() { h.admin("drop database if exists " + db) })
	return h
}

func (h *pgHarness) admin(sql string) {
	args := append(strings.Fields(h.conn), "-X", "-q", "-d", "postgres", "-c", sql)
	if out, err := exec.Command("psql", args...).CombinedOutput(); err != nil {
		h.t.Fatalf("admin %q: %v\n%s", sql, err, out)
	}
}

func (h *pgHarness) query(sql string) string {
	args := append(strings.Fields(h.conn), "-X", "-At", "-d", h.db, "-c", sql)
	out, err := exec.Command("psql", args...).CombinedOutput()
	if err != nil {
		h.t.Fatalf("query %q: %v\n%s", sql, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (h *pgHarness) file(name, body string) {
	if err := os.WriteFile(filepath.Join(h.migDir, name), []byte(body), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// run plans, packages and executes the runner like a deploy would.
func (h *pgHarness) run(env []string, args ...string) (stdout, stderr string, err error) {
	files, perr := Plan(h.migDir)
	if perr != nil {
		h.t.Fatal(perr)
	}
	if err := PackageArchive(h.migDir, files, filepath.Join(h.runDir, ArchiveName)); err != nil {
		h.t.Fatal(err)
	}
	script := RenderRunner(files, RunnerOptions{Container: "testpg", Database: h.db, User: "postgres", Backup: true, BackupDir: h.backups, KeepBackups: 2})
	if err := os.WriteFile(filepath.Join(h.runDir, RunnerName), []byte(script), 0o755); err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{filepath.Join(h.runDir, RunnerName)}, args...)...)
	cmd.Dir = h.runDir
	cmd.Env = append(append(os.Environ(), "PATH="+h.bin+":"+os.Getenv("PATH")), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

func (h *pgHarness) mustRun(env []string, args ...string) string {
	out, errOut, err := h.run(env, args...)
	if err != nil {
		h.t.Fatalf("runner %v failed: %v\n%s", args, err, errOut)
	}
	return out
}

func (h *pgHarness) mustFail(contains string, args ...string) {
	_, errOut, err := h.run(nil, args...)
	if err == nil {
		h.t.Fatalf("runner %v should fail", args)
	}
	if !strings.Contains(errOut, contains) {
		h.t.Fatalf("runner %v: expected %q in\n%s", args, contains, errOut)
	}
}

func backupCount(t *testing.T, dir string) int {
	matches, _ := filepath.Glob(filepath.Join(dir, "pre-migrate-*.dump"))
	return len(matches)
}

func TestPGFreshDatabaseLifecycle(t *testing.T) {
	h := newPGHarness(t, "eu_migrate_fresh")
	h.file("001_items.sql", "create table items (id serial primary key, name text);")
	h.file("002_seed.sql", "insert into items (name) values ('a'), ('b');")

	out := h.mustRun([]string{"EU_MIGRATE_WHO=deploy o'neil"}, "apply")
	if !strings.Contains(out, "eu-migrate-result\tapply\t2\t0\t2\t-") {
		t.Fatalf("fresh apply result: %q", out)
	}
	if got := h.query("select count(*) from items"); got != "2" {
		t.Fatalf("items = %s", got)
	}
	if got := h.query("select how from schema_migrations where filename = '002_seed.sql'"); got != "deploy o'neil" {
		t.Fatalf("how = %q", got)
	}
	if backupCount(t, h.backups) != 0 {
		t.Fatal("fresh database should not be backed up")
	}

	// Up to date: nothing runs.
	if out := h.mustRun(nil, "apply"); !strings.Contains(out, "\tapply\t2\t0\t0\t") {
		t.Fatalf("no-op apply: %q", out)
	}

	// Destructive file is refused before anything runs.
	h.file("003_cleanup.sql", "delete from items where name = 'a';")
	h.mustFail("contains DROP/TRUNCATE/DELETE", "apply")
	if got := h.query("select count(*) from items"); got != "2" {
		t.Fatal("refused file must not run")
	}
	if out := h.mustRun(nil, "status"); !strings.Contains(out, "\tstatus\t2\t1\t") {
		t.Fatalf("status: %q", out)
	}

	// Opted in: runs, with a backup first.
	h.file("003_cleanup.sql", "-- allow-destructive\ndelete from items where name = 'a';")
	out = h.mustRun(nil, "apply")
	if !strings.Contains(out, "\tapply\t3\t0\t1\t"+h.backups+"/pre-migrate-") {
		t.Fatalf("apply with backup: %q", out)
	}
	if backupCount(t, h.backups) != 1 || h.query("select count(*) from items") != "1" {
		t.Fatal("expected one backup and one item left")
	}

	// A failing file is rolled back entirely and stops later files.
	h.file("004_broken.sql", "create table half_done (id int);\nselect no_such_function();")
	h.file("005_after.sql", "create table after_broken (id int);")
	h.mustFail("004_broken.sql failed and was rolled back", "apply")
	if h.query("select count(*) from pg_tables where tablename in ('half_done','after_broken')") != "0" {
		t.Fatal("failed migration left tables behind")
	}
	if h.query("select count(*) from schema_migrations") != "3" {
		t.Fatal("failed migration was recorded")
	}

	// Fixed: a file with its own transaction plus the next one.
	h.file("004_broken.sql", "begin;\ncreate table half_done (id int);\ncommit;")
	h.mustRun(nil, "apply")
	if h.query("select count(*) from schema_migrations") != "5" {
		t.Fatal("expected 5 recorded files")
	}

	// Backups are pruned to keep_backups (2).
	for i := 6; i <= 8; i++ {
		h.file(fmt.Sprintf("%03d_more.sql", i), fmt.Sprintf("create table more_%d (id int);", i))
		h.mustRun(nil, "apply")
	}
	if n := backupCount(t, h.backups); n != 2 {
		t.Fatalf("expected 2 backups after pruning, got %d", n)
	}
}

func TestPGExistingDatabaseNeedsBaseline(t *testing.T) {
	h := newPGHarness(t, "eu_migrate_existing")
	h.query("create table legacy (id int)")
	h.file("001_legacy.sql", "create table legacy (id int);")
	h.file("002_new.sql", "alter table legacy add column name text;")

	h.mustFail("Run 'eu migrate baseline' once", "apply")
	if h.query("select count(*) from schema_migrations") != "0" {
		t.Fatal("refused apply must not record anything")
	}
	h.mustFail("not one of the migration files", "baseline", "nope.sql")

	out := h.mustRun(nil, "baseline", "001_legacy.sql")
	if !strings.Contains(out, "\tbaseline\t1\t1\t") {
		t.Fatalf("baseline: %q", out)
	}
	h.mustFail("baseline already done", "baseline")

	h.mustRun(nil, "apply")
	if h.query("select count(*) from information_schema.columns where table_name = 'legacy' and column_name = 'name'") != "1" {
		t.Fatal("002_new.sql did not run")
	}
	if h.query("select string_agg(how, ',' order by filename) from schema_migrations") != "baseline,eu migrate" {
		t.Fatal("unexpected bookkeeping")
	}

	// Edited after apply: reported by status, never re-run.
	h.file("002_new.sql", "alter table legacy add column name text; -- edited")
	_, errOut, err := h.run(nil, "status")
	if err != nil || !strings.Contains(errOut, "002_new.sql changed after it was applied") {
		t.Fatalf("status should report the edit: %v\n%s", err, errOut)
	}
}
