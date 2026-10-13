package main

import (
	"testing"

	"github.com/sorisltd/eu-deploy/internal/config"
	"github.com/sorisltd/eu-deploy/internal/deploy"
)

func TestResolveMigrationOptionsShared(t *testing.T) {
	cfg := config.Config{Database: &config.DatabaseSpec{
		Mode:       "shared",
		Shared:     &config.SharedDatabaseSpec{Name: "shop", User: "shop"},
		Migrations: &config.MigrationsSpec{Dir: "db/migrations"},
	}}
	opts, err := resolveMigrationOptions(cfg, "/work", deploy.RemoteTargetHetzner, "/opt/eu-deploy", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Dir != "/work/db/migrations" || opts.Container != "eu-shared-postgres" || opts.Database != "shop" || opts.User != "shop" {
		t.Fatalf("unexpected %+v", opts)
	}
	if !opts.Backup || opts.KeepBackups != 10 || opts.BackupDir != "/opt/eu-deploy/backups/shop" {
		t.Fatalf("unexpected backup defaults %+v", opts)
	}
}

func TestResolveMigrationOptionsExplicit(t *testing.T) {
	off := false
	cfg := config.Config{Database: &config.DatabaseSpec{
		Mode:       "external",
		Migrations: &config.MigrationsSpec{Dir: "supabase/migrations", Container: "eu-safebuild-postgres", Database: "bustora", Backup: &off},
	}}
	opts, err := resolveMigrationOptions(cfg, "/work", deploy.RemoteTargetHetzner, "/opt/eu-deploy", "bustora")
	if err != nil {
		t.Fatal(err)
	}
	if opts.User != "postgres" || opts.Backup || opts.Container != "eu-safebuild-postgres" {
		t.Fatalf("unexpected %+v", opts)
	}

	cfg.Database.Migrations.Container = ""
	if _, err := resolveMigrationOptions(cfg, "/work", deploy.RemoteTargetHetzner, "/opt/eu-deploy", "bustora"); err == nil {
		t.Fatal("missing container outside shared mode should fail")
	}

	none, err := resolveMigrationOptions(config.Config{}, "/work", deploy.RemoteTargetHetzner, "/opt/eu-deploy", "x")
	if err != nil || none != nil {
		t.Fatal("no migrations configured should resolve to nil")
	}
}
