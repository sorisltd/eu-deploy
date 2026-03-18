package analytics

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/lib/pq"
)

type Config struct {
	ServerRoot string
	LogsDir    string
	AppsDir    string
	SecretFile string
	CityDBPath string
	ASNDBPath  string
}

func DefaultConfig(serverRoot string) Config {
	root := strings.TrimSpace(serverRoot)
	if root == "" {
		root = "/opt/eu-deploy"
	}

	analyticsRoot := filepath.Join(root, "analytics")
	maxmindRoot := filepath.Join(analyticsRoot, "maxmind")

	return Config{
		ServerRoot: root,
		LogsDir:    "/var/log/caddy",
		AppsDir:    filepath.Join(root, "apps"),
		SecretFile: filepath.Join(analyticsRoot, "analytics.secret"),
		CityDBPath: filepath.Join(maxmindRoot, "GeoLite2-City.mmdb"),
		ASNDBPath:  filepath.Join(maxmindRoot, "GeoLite2-ASN.mmdb"),
	}
}

func OpenDatabase(cfg Config) (*sql.DB, error) {
	values, err := readSimpleEnv(filepath.Join(cfg.ServerRoot, "_postgres", "postgres.env"))
	if err != nil {
		return nil, err
	}

	password := strings.TrimSpace(values["POSTGRES_PASSWORD"])
	if password == "" {
		return nil, fmt.Errorf("POSTGRES_PASSWORD is missing from %s", filepath.Join(cfg.ServerRoot, "_postgres", "postgres.env"))
	}

	dsn := fmt.Sprintf(
		"host=127.0.0.1 port=5432 user=postgres password=%s dbname=postgres sslmode=disable",
		password,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func EnsureSecretFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("secret file path is required")
	}

	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return err
	}

	value := hex.EncodeToString(bytes)
	return os.WriteFile(path, []byte(value+"\n"), 0o600)
}

func ReadSecret(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	value := strings.TrimSpace(string(contents))
	if value == "" {
		return "", fmt.Errorf("analytics secret is empty: %s", path)
	}

	return value, nil
}

func readSimpleEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return values, nil
}
