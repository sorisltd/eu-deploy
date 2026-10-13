package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeployScriptMigratesAfterBuildBeforeSwitch(t *testing.T) {
	opts := RemoteOptions{
		RemoteAppPath:    "/opt/eu-deploy/apps/app",
		RemoteServerPath: "/opt/eu-deploy",
		AppContainerName: "eu-app-app",
		ImageTag:         "eu-deploy-app:remote",
		ContainerPort:    3000,
		ServicePort:      3100,
		ReleaseID:        "r1",
		Hostnames:        []string{"app.example.com"},
		RoutePath:        "/",
		SiteConfigName:   "app.caddy",
		Migrations:       &MigrationOptions{Dir: "db", Container: "c", Database: "d", User: "u"},
	}
	script := renderHetznerDeployScript(opts)
	build := strings.Index(script, "docker build -t")
	migrate := strings.Index(script, "EU_MIGRATE_WHO='deploy r1' bash migrate.sh apply")
	run := strings.Index(script, "docker run -d --restart unless-stopped")
	if build < 0 || migrate < 0 || run < 0 || !(build < migrate && migrate < run) {
		t.Fatalf("expected build < migrate < run, got %d %d %d", build, migrate, run)
	}
	if !strings.Contains(script, "cp migrations.tar.gz migrate.sh") {
		t.Fatal("migration files are not copied into the release folder")
	}

	opts.Migrations = nil
	if strings.Contains(renderHetznerDeployScript(opts), "migrate.sh") {
		t.Fatal("no migrations configured, but the script runs them")
	}
}

func TestWriteMigrationBundle(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "001.sql"), []byte("create table a();"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	m := MigrationOptions{Dir: src, Container: "c", Database: "d", User: "u", Backup: true, BackupDir: "/b"}
	if err := writeMigrationBundle(m, out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"migrations.tar.gz", "migrate.sh"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("%s not written: %v", name, err)
		}
	}
	m.Container = ""
	if err := writeMigrationBundle(m, out); err == nil {
		t.Fatal("missing container should fail")
	}
}

func TestParseMigrationResult(t *testing.T) {
	res, err := parseMigrationResult(strings.NewReader("noise\neu-migrate-result\tapply\t96\t0\t2\t/b/pre.dump\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "apply" || res.Applied != 96 || res.Ran != 2 || res.Backup != "/b/pre.dump" {
		t.Fatalf("unexpected %+v", res)
	}
	res, err = parseMigrationResult(strings.NewReader("eu-migrate-result\tstatus\t5\t1\t0\t-\n"))
	if err != nil || res.Backup != "" || res.Pending != 1 {
		t.Fatalf("unexpected %+v %v", res, err)
	}
	if _, err := parseMigrationResult(strings.NewReader("nothing")); err == nil {
		t.Fatal("missing line should fail")
	}
}

func TestStaticRuntimeRejectsMigrations(t *testing.T) {
	opts := RemoteOptions{RuntimeType: "static", Migrations: &MigrationOptions{},
		WorkDir: "/tmp", RemoteHost: "h", RemoteUser: "root", RemotePort: 22, Hostname: "x.example.com", ProxyContainerName: "p", SiteConfigName: "s", RoutePath: "/", RemoteServerPath: "/opt/eu-deploy", RemoteAppPath: "/opt/eu-deploy/apps/x"}
	err := validateRemoteDeployOptions(opts)
	if err == nil || !strings.Contains(err.Error(), "database.migrations") {
		t.Fatalf("expected a migrations error, got %v", err)
	}
}
