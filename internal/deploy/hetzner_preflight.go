package deploy

import (
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type PreflightStatus string

const (
	PreflightOK      PreflightStatus = "ok"
	PreflightWarning PreflightStatus = "warning"
	PreflightFailure PreflightStatus = "failure"
)

type PreflightResult struct {
	Name   string
	Status PreflightStatus
	Detail string
}

func PreflightHetzner(opts HetznerOptions) ([]PreflightResult, error) {
	results := []PreflightResult{}

	results = append(results, executableCheck("ssh")...)
	results = append(results, executableCheck("scp")...)

	serverIPs, serverErr := resolveHostAddresses(opts.RemoteHost)
	if serverErr != nil {
		results = append(results, PreflightResult{
			Name:   "Server address",
			Status: PreflightFailure,
			Detail: serverErr.Error(),
		})
	} else {
		results = append(results, PreflightResult{
			Name:   "Server address",
			Status: PreflightOK,
			Detail: strings.Join(serverIPs, ", "),
		})
	}

	hostnameIPs, hostnameErr := resolveHostAddresses(opts.Hostname)
	if hostnameErr != nil {
		results = append(results, PreflightResult{
			Name:   "Hostname DNS",
			Status: PreflightFailure,
			Detail: hostnameErr.Error(),
		})
	} else {
		results = append(results, PreflightResult{
			Name:   "Hostname DNS",
			Status: PreflightOK,
			Detail: strings.Join(hostnameIPs, ", "),
		})
	}

	dnsMatches := false
	if serverErr == nil && hostnameErr == nil {
		dnsMatches = addressesIntersect(serverIPs, hostnameIPs)
		status := PreflightWarning
		detail := "hostname does not currently resolve to the Hetzner server address"
		if dnsMatches {
			status = PreflightOK
			detail = "hostname resolves to the Hetzner server"
		}
		results = append(results, PreflightResult{
			Name:   "DNS match",
			Status: status,
			Detail: detail,
		})
	}

	sshOut, err := runRemoteCommandCapture(opts, true, "printf connected")
	if err != nil {
		results = append(results, PreflightResult{
			Name:   "SSH connectivity",
			Status: PreflightFailure,
			Detail: strings.TrimSpace(err.Error()),
		})
		return results, nil
	}
	results = append(results, PreflightResult{
		Name:   "SSH connectivity",
		Status: PreflightOK,
		Detail: strings.TrimSpace(sshOut),
	})

	results = append(results, remoteBooleanCheck(opts, "Docker CLI", "command -v docker >/dev/null 2>&1", "docker is installed", "docker is not installed")...)
	results = append(results, remoteBooleanCheck(opts, "Docker daemon", "docker info >/dev/null 2>&1", "docker daemon is reachable", "docker daemon is not reachable")...)
	results = append(results, remoteBooleanCheck(opts, "Server paths", fmt.Sprintf("mkdir -p %s %s && test -w %s && test -w %s",
		shellQuote(opts.RemoteServerPath),
		shellQuote(opts.RemoteAppPath),
		shellQuote(opts.RemoteServerPath),
		shellQuote(opts.RemoteAppPath)),
		"server paths are writable",
		"cannot create or write to server paths")...)

	portsStatus, err := runRemoteCommandCapture(opts, true, renderRemoteSharedProxyCheck(opts.ProxyContainerName))
	if err != nil {
		results = append(results, PreflightResult{
			Name:   "Ports 80/443",
			Status: PreflightWarning,
			Detail: strings.TrimSpace(err.Error()),
		})
	} else {
		results = append(results, interpretSharedProxyCheck(strings.TrimSpace(portsStatus)))
	}

	serviceStatus, err := runRemoteCommandCapture(opts, true, renderRemoteServicePortCheck(opts.ServicePort, opts.AppContainerName))
	if err != nil {
		results = append(results, PreflightResult{
			Name:   "Service port",
			Status: PreflightWarning,
			Detail: strings.TrimSpace(err.Error()),
		})
	} else {
		results = append(results, interpretServicePortCheck(strings.TrimSpace(serviceStatus), opts.ServicePort))
	}

	tlsStatus := PreflightWarning
	tlsDetail := "cannot prove certificate issuance before deploy, but the prerequisites are not fully in place yet"
	if dnsMatches && hasAcceptableProxyState(results) {
		tlsStatus = PreflightOK
		tlsDetail = "DNS points at the server and ports 80/443 look available for the shared proxy"
	}
	results = append(results, PreflightResult{
		Name:   "TLS prerequisites",
		Status: tlsStatus,
		Detail: tlsDetail,
	})

	return results, nil
}

func executableCheck(name string) []PreflightResult {
	if _, err := exec.LookPath(name); err != nil {
		return []PreflightResult{{
			Name:   fmt.Sprintf("Local %s", name),
			Status: PreflightFailure,
			Detail: fmt.Sprintf("%s is not installed locally", name),
		}}
	}
	return []PreflightResult{{
		Name:   fmt.Sprintf("Local %s", name),
		Status: PreflightOK,
		Detail: fmt.Sprintf("%s is installed locally", name),
	}}
}

func remoteBooleanCheck(opts HetznerOptions, name, command, okDetail, failDetail string) []PreflightResult {
	out, err := runRemoteCommandCapture(opts, true, command)
	if err != nil {
		return []PreflightResult{{
			Name:   name,
			Status: PreflightFailure,
			Detail: strings.TrimSpace(err.Error()),
		}}
	}

	if strings.TrimSpace(out) == "" {
		return []PreflightResult{{
			Name:   name,
			Status: PreflightOK,
			Detail: okDetail,
		}}
	}

	return []PreflightResult{{
		Name:   name,
		Status: PreflightWarning,
		Detail: strings.TrimSpace(out),
	}}
}

func runRemoteCommandCapture(opts HetznerOptions, batchMode bool, command string) (string, error) {
	args := buildSSHArgs(opts, batchMode)
	args = append(args, fmt.Sprintf("%s@%s", opts.RemoteUser, opts.RemoteHost), "bash", "-lc", command)

	cmd := exec.Command("ssh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func resolveHostAddresses(host string) ([]string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("host is empty")
	}
	if ip := net.ParseIP(host); ip != nil {
		return []string{ip.String()}, nil
	}

	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s: %w", host, err)
	}
	seen := map[string]struct{}{}
	resolved := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		resolved = append(resolved, addr)
	}
	sort.Strings(resolved)
	return resolved, nil
}

func addressesIntersect(left, right []string) bool {
	set := map[string]struct{}{}
	for _, item := range left {
		set[item] = struct{}{}
	}
	for _, item := range right {
		if _, ok := set[item]; ok {
			return true
		}
	}
	return false
}

func renderRemoteSharedProxyCheck(proxyContainerName string) string {
	return strings.Join([]string{
		"if docker ps --format '{{.Names}}' | grep -Fx -- " + shellQuote(proxyContainerName) + " >/dev/null 2>&1; then",
		"  echo proxy",
		"elif command -v ss >/dev/null 2>&1 && ss -ltn '( sport = :80 or sport = :443 )' | tail -n +2 | grep -q .; then",
		"  echo busy",
		"else",
		"  echo free",
		"fi",
	}, "\n")
}

func renderRemoteServicePortCheck(port int, appContainerName string) string {
	portString := strconv.Itoa(port)
	return strings.Join([]string{
		"if docker ps --format '{{.Names}}' | grep -Fx -- " + shellQuote(appContainerName) + " >/dev/null 2>&1; then",
		"  echo app-container",
		"elif command -v ss >/dev/null 2>&1 && ss -ltn '( sport = :" + portString + " )' | tail -n +2 | grep -q .; then",
		"  echo busy",
		"else",
		"  echo free",
		"fi",
	}, "\n")
}

func interpretSharedProxyCheck(status string) PreflightResult {
	switch status {
	case "proxy":
		return PreflightResult{
			Name:   "Ports 80/443",
			Status: PreflightOK,
			Detail: "shared proxy is already running on ports 80/443",
		}
	case "free":
		return PreflightResult{
			Name:   "Ports 80/443",
			Status: PreflightOK,
			Detail: "ports 80/443 are free for the shared proxy",
		}
	default:
		return PreflightResult{
			Name:   "Ports 80/443",
			Status: PreflightFailure,
			Detail: "ports 80/443 are already in use by something other than the shared proxy",
		}
	}
}

func interpretServicePortCheck(status string, port int) PreflightResult {
	switch status {
	case "app-container":
		return PreflightResult{
			Name:   "Service port",
			Status: PreflightOK,
			Detail: fmt.Sprintf("the current app container already owns 127.0.0.1:%d", port),
		}
	case "free":
		return PreflightResult{
			Name:   "Service port",
			Status: PreflightOK,
			Detail: fmt.Sprintf("127.0.0.1:%d is free", port),
		}
	default:
		return PreflightResult{
			Name:   "Service port",
			Status: PreflightFailure,
			Detail: fmt.Sprintf("127.0.0.1:%d is already in use by something else", port),
		}
	}
}

func hasAcceptableProxyState(results []PreflightResult) bool {
	for _, result := range results {
		if result.Name != "Ports 80/443" {
			continue
		}
		return result.Status == PreflightOK
	}
	return false
}
