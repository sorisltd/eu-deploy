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
}

func PromptHetznerBootstrapOptions(spec config.HetznerSpec) (HetznerBootstrapOptions, error) {
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

	return HetznerBootstrapOptions{
		RemoteHost:       spec.Host,
		RemoteUser:       spec.User,
		RemotePort:       spec.Port,
		SSHKeyPath:       spec.SSHKeyPath,
		RemoteServerPath: effectiveHetznerServerPath(spec, ""),
		RemoteAppPath:    spec.AppPath,
		InstallUFW:       installUFW,
		InstallFail2ban:  installFail2ban,
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
