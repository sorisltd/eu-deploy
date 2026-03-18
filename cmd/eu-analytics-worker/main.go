package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sorisltd/eu-deploy/internal/analytics"
)

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: eu-analytics-worker <init-schema|process|aggregate> [flags]")
	}

	switch os.Args[1] {
	case "init-schema":
		runInitSchema(os.Args[2:])
	case "process":
		runProcess(os.Args[2:])
	case "aggregate":
		runAggregate(os.Args[2:])
	default:
		fatalf("unknown command: %s", os.Args[1])
	}
}

func runInitSchema(args []string) {
	cfg := configFromFlags(args, false)
	db, err := analytics.OpenDatabase(cfg)
	if err != nil {
		fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := analytics.EnsureSchema(context.Background(), db); err != nil {
		fatalf("init schema: %v", err)
	}
	if err := analytics.EnsureSecretFile(cfg.SecretFile); err != nil {
		fatalf("ensure secret: %v", err)
	}
}

func runProcess(args []string) {
	cfg := configFromFlags(args, true)
	if err := analytics.Process(context.Background(), cfg); err != nil {
		fatalf("process logs: %v", err)
	}
}

func runAggregate(args []string) {
	fs := flag.NewFlagSet("aggregate", flag.ExitOnError)
	serverRoot := fs.String("server-root", "/opt/eu-deploy", "Remote eu-deploy server root")
	targetDate := fs.String("date", "", "UTC date to aggregate in YYYY-MM-DD format (default: yesterday)")
	logsDir := fs.String("logs-dir", "", "Ignored for aggregate; accepted for wrapper symmetry")
	secretFile := fs.String("secret-file", "", "Ignored for aggregate; accepted for wrapper symmetry")
	cityDBPath := fs.String("city-db", "", "Ignored for aggregate; accepted for wrapper symmetry")
	asnDBPath := fs.String("asn-db", "", "Ignored for aggregate; accepted for wrapper symmetry")
	appsDir := fs.String("apps-dir", "", "Ignored for aggregate; accepted for wrapper symmetry")
	_ = logsDir
	_ = secretFile
	_ = cityDBPath
	_ = asnDBPath
	_ = appsDir
	if err := fs.Parse(args); err != nil {
		fatalf("parse flags: %v", err)
	}

	cfg := analytics.DefaultConfig(*serverRoot)
	date := time.Now().UTC().Add(-24 * time.Hour)
	if value := *targetDate; value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			fatalf("invalid aggregate date %q: %v", value, err)
		}
		date = parsed
	}

	if err := analytics.Aggregate(context.Background(), cfg, date); err != nil {
		fatalf("aggregate analytics: %v", err)
	}
}

func configFromFlags(args []string, includeProcessPaths bool) analytics.Config {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	serverRoot := fs.String("server-root", "/opt/eu-deploy", "Remote eu-deploy server root")
	logsDir := fs.String("logs-dir", "/var/log/caddy", "Directory containing Caddy access logs")
	appsDir := fs.String("apps-dir", "", "Override app directory (default: <server-root>/apps)")
	secretFile := fs.String("secret-file", "", "Override analytics secret path")
	cityDBPath := fs.String("city-db", "", "Override GeoLite2 City database path")
	asnDBPath := fs.String("asn-db", "", "Override GeoLite2 ASN database path")
	if err := fs.Parse(args); err != nil {
		fatalf("parse flags: %v", err)
	}

	cfg := analytics.DefaultConfig(*serverRoot)
	if *logsDir != "" {
		cfg.LogsDir = *logsDir
	}
	if *appsDir != "" {
		cfg.AppsDir = *appsDir
	}
	if *secretFile != "" {
		cfg.SecretFile = *secretFile
	}
	if *cityDBPath != "" {
		cfg.CityDBPath = *cityDBPath
	}
	if *asnDBPath != "" {
		cfg.ASNDBPath = *asnDBPath
	}
	if !includeProcessPaths {
		// Keep the compiler happy for flags shared with init-schema.
		_ = cfg.LogsDir
		_ = cfg.AppsDir
		_ = cfg.CityDBPath
		_ = cfg.ASNDBPath
	}

	return cfg
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
