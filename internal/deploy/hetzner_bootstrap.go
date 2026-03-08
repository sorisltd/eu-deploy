package deploy

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/config"
)

type HetznerBootstrapOptions struct {
	RemoteHost       string
	RemoteUser       string
	RemotePort       int
	SSHKeyPath       string
	RemoteServerPath string
	RemoteAppPath    string
	InstallUFW       bool
	InstallFail2ban  bool
	SharedDatabase   *SharedDatabaseOptions
}

func PromptHetznerBootstrapOptions(cfg config.Config) (HetznerBootstrapOptions, error) {
	p := &linePrompter{
		in:  bufio.NewReader(os.Stdin),
		out: os.Stdout,
	}

	installUFW, err := p.Bool("Install and configure UFW firewall", true)
	if err != nil {
		return HetznerBootstrapOptions{}, err
	}
	installFail2ban, err := p.Bool("Install fail2ban", false)
	if err != nil {
		return HetznerBootstrapOptions{}, err
	}

	var sharedDatabase *SharedDatabaseOptions
	if usesSharedDatabase(cfg.Database) {
		installSharedPostgres, err := p.Bool("Install and start shared PostgreSQL", true)
		if err != nil {
			return HetznerBootstrapOptions{}, err
		}
		if installSharedPostgres {
			sharedDatabase = &SharedDatabaseOptions{
				Version: cfg.Database.Shared.Version,
				Name:    cfg.Database.Shared.Name,
				User:    cfg.Database.Shared.User,
			}
		}
	}

	return HetznerBootstrapOptions{
		RemoteHost:       cfg.Hetzner.Host,
		RemoteUser:       cfg.Hetzner.User,
		RemotePort:       cfg.Hetzner.Port,
		SSHKeyPath:       cfg.Hetzner.SSHKeyPath,
		RemoteServerPath: effectiveHetznerServerPath(*cfg.Hetzner, ""),
		RemoteAppPath:    cfg.Hetzner.AppPath,
		InstallUFW:       installUFW,
		InstallFail2ban:  installFail2ban,
		SharedDatabase:   sharedDatabase,
	}, nil
}

func BootstrapHetzner(opts HetznerBootstrapOptions) error {
	if strings.TrimSpace(opts.RemoteHost) == "" {
		return fmt.Errorf("hetzner.host is required")
	}
	if strings.TrimSpace(opts.RemoteUser) == "" {
		return fmt.Errorf("hetzner.user is required")
	}
	if opts.RemotePort <= 0 {
		return fmt.Errorf("hetzner.port is required")
	}
	if strings.TrimSpace(opts.RemoteServerPath) == "" {
		return fmt.Errorf("hetzner.server_path is required")
	}
	if strings.TrimSpace(opts.RemoteAppPath) == "" {
		return fmt.Errorf("hetzner.app_path is required")
	}

	script := renderHetznerBootstrapScript(opts)
	return runRemoteScript(HetznerOptions{
		RemoteHost: opts.RemoteHost,
		RemoteUser: opts.RemoteUser,
		RemotePort: opts.RemotePort,
		SSHKeyPath: opts.SSHKeyPath,
	}, script)
}

func renderHetznerBootstrapScript(opts HetznerBootstrapOptions) string {
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	lines := []string{
		"set -euo pipefail",
		"SUDO=''",
		"if [ \"$(id -u)\" -ne 0 ]; then",
		"  if ! command -v sudo >/dev/null 2>&1; then",
		"    echo 'sudo is required for bootstrap when not connected as root' >&2",
		"    exit 1",
		"  fi",
		"  SUDO='sudo'",
		"fi",
		"if ! command -v apt-get >/dev/null 2>&1; then",
		"  echo 'bootstrap currently supports Debian/Ubuntu style hosts with apt-get' >&2",
		"  exit 1",
		"fi",
		"if ! command -v docker >/dev/null 2>&1; then",
		"  $SUDO apt-get update",
		"  $SUDO apt-get install -y ca-certificates curl",
		"  curl -fsSL https://get.docker.com | $SUDO sh",
		"fi",
		"$SUDO systemctl enable --now docker",
		"if [ \"$(id -u)\" -ne 0 ]; then",
		"  $SUDO usermod -aG docker \"$(id -un)\" || true",
		"fi",
		fmt.Sprintf("$SUDO mkdir -p %s", shellQuote(opts.RemoteServerPath)),
		fmt.Sprintf("$SUDO mkdir -p %s", shellQuote(opts.RemoteAppPath)),
		fmt.Sprintf("$SUDO mkdir -p %s", shellQuote(filepathJoinSlash(proxyRoot, "sites"))),
		fmt.Sprintf("$SUDO mkdir -p %s", shellQuote(filepathJoinSlash(proxyRoot, "data"))),
		fmt.Sprintf("$SUDO mkdir -p %s", shellQuote(filepathJoinSlash(proxyRoot, "config"))),
		fmt.Sprintf("$SUDO docker network inspect %s >/dev/null 2>&1 || $SUDO docker network create %s >/dev/null",
			shellQuote(sharedDockerNetwork),
			shellQuote(sharedDockerNetwork)),
	}

	if opts.SharedDatabase != nil {
		lines = append(lines, renderSharedPostgresBootstrapCommands(opts.RemoteServerPath, *opts.SharedDatabase)...)
	}

	if opts.InstallUFW {
		lines = append(lines,
			"$SUDO apt-get install -y ufw",
			"$SUDO ufw allow OpenSSH",
			"$SUDO ufw allow 80/tcp",
			"$SUDO ufw allow 443/tcp",
			"$SUDO ufw --force enable",
		)
	}
	if opts.InstallFail2ban {
		lines = append(lines,
			"$SUDO apt-get install -y fail2ban",
			"$SUDO systemctl enable --now fail2ban",
		)
	}

	return strings.Join(lines, "\n")
}

func filepathJoinSlash(parts ...string) string {
	return strings.ReplaceAll(strings.Join(parts, "/"), "//", "/")
}

func renderSharedPostgresBootstrapCommands(serverPath string, opts SharedDatabaseOptions) []string {
	postgresRoot := sharedPostgresRoot(serverPath)
	postgresDataPath := filepathJoinSlash(postgresRoot, "data")
	postgresEnvPath := filepathJoinSlash(postgresRoot, "postgres.env")

	return []string{
		fmt.Sprintf("mkdir -p %s", shellQuote(postgresRoot)),
		fmt.Sprintf("mkdir -p %s", shellQuote(postgresDataPath)),
		fmt.Sprintf("if [ ! -f %s ]; then", shellQuote(postgresEnvPath)),
		"  POSTGRES_PASSWORD=$(od -An -tx1 -N24 /dev/urandom | tr -d ' \\n')",
		"  POSTGRES_PASSWORD=${POSTGRES_PASSWORD:0:40}",
		fmt.Sprintf("  umask 077 && printf 'POSTGRES_PASSWORD=%%s\\n' \"$POSTGRES_PASSWORD\" > %s", shellQuote(postgresEnvPath)),
		"fi",
		fmt.Sprintf("$SUDO docker pull %s >/dev/null", shellQuote(sharedPostgresImage(opts))),
		fmt.Sprintf("if ! $SUDO docker ps -a --format '{{.Names}}' | grep -Fx -- %s >/dev/null 2>&1; then", shellQuote(sharedPostgresContainer)),
		fmt.Sprintf("  $SUDO docker run -d --restart unless-stopped --network %s -p 127.0.0.1:5432:5432 --name %s --env-file %s -v %s:/var/lib/postgresql/data %s >/dev/null",
			shellQuote(sharedDockerNetwork),
			shellQuote(sharedPostgresContainer),
			shellQuote(postgresEnvPath),
			shellQuote(postgresDataPath),
			shellQuote(sharedPostgresImage(opts))),
		"else",
		fmt.Sprintf("  $SUDO docker start %s >/dev/null 2>&1 || true", shellQuote(sharedPostgresContainer)),
		"fi",
		fmt.Sprintf("$SUDO docker network connect %s %s >/dev/null 2>&1 || true",
			shellQuote(sharedDockerNetwork),
			shellQuote(sharedPostgresContainer)),
	}
}
