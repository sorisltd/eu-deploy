package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsDestructive(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want bool
	}{
		{"create table", "create table a (id int);", false},
		{"drop table", "DROP TABLE a;", true},
		{"drop table if exists", "drop table if exists a;", true},
		{"truncate", "truncate a;", true},
		{"delete from", "delete from a where true;", true},
		{"drop column", "alter table a drop column b;", true},
		{"drop column short form", "alter table a drop b;", true},
		{"drop constraint is fine", "alter table a drop constraint a_pkey;", false},
		{"drop index is fine", "drop index if exists a_idx;", false},
		{"drop function is fine", "drop function if exists f();", false},
		{"opt out", "-- allow-destructive\ndrop table a;", false},
		{"comment only", "-- drop table a;\ncreate table b();", false},
		{"string literal", "comment on table a is 'we never drop table a';", false},
		{"grant truncate", "grant select, insert, truncate on a to app;", false},
		{"function body", "create function f() returns void language plpgsql as $fn$ begin delete from a; end $fn$;", false},
		{"function body with empty tag", "create function f() returns void language sql as $$ delete from a $$;", false},
		{"do block runs now", "do $$ begin delete from a; end $$;", true},
		{"after a function body", "create function f() returns int as $$ select 1 $$ language sql;\ntruncate b;", true},
		{"multiline drop", "drop\n  table\n  a;", true},
	}
	for _, c := range cases {
		if got := IsDestructive(c.sql); got != c.want {
			t.Errorf("%s: IsDestructive = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHasOwnTransaction(t *testing.T) {
	if !HasOwnTransaction("-- x\nBEGIN;\ncreate table a();\ncommit;") {
		t.Error("BEGIN; on its own line should count")
	}
	if HasOwnTransaction("create function f() as $$ begin return; end $$;") {
		t.Error("plpgsql begin is not a transaction")
	}
}

func TestPlanSortsAndValidates(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("002_b.sql", "drop table a;")
	write("001_a.sql", "begin;\ncreate table a();\ncommit;")
	write("README.md", "ignored")
	files, err := Plan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "001_a.sql" || files[1].Name != "002_b.sql" {
		t.Fatalf("unexpected plan: %+v", files)
	}
	if !files[0].OwnTx || files[0].Destructive || !files[1].Destructive || len(files[0].SHA256) != 64 {
		t.Fatalf("unexpected flags: %+v", files)
	}

	write("003 bad'name.sql", "select 1;")
	if _, err := Plan(dir); err == nil || !strings.Contains(err.Error(), "file name") {
		t.Fatalf("expected file name error, got %v", err)
	}

	if _, err := Plan(t.TempDir()); err == nil {
		t.Fatal("empty folder should fail")
	}
}

func TestRenderRunnerEmbedsPlan(t *testing.T) {
	script := RenderRunner([]File{{Name: "001_a.sql", SHA256: "abc", OwnTx: true}}, RunnerOptions{
		Container: "eu-shared-postgres", Database: "app", User: "app", Backup: true, BackupDir: "/opt/eu-deploy/backups/app",
	})
	for _, want := range []string{
		"CONTAINER='eu-shared-postgres'",
		"DB='app'",
		"BACKUP=1",
		"KEEP=10",
		"001_a.sql\tabc\ttrue\tfalse\n",
		"create table if not exists public.schema_migrations",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("runner is missing %q", want)
		}
	}
	if strings.Contains(script, "%!") {
		t.Error("runner has a formatting error")
	}
}
