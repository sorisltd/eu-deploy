package deploy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func LogsRemote(opts RemoteOptions, component string, follow bool, tail int) error {
	if isStaticRuntime(opts.RuntimeType) {
		return fmt.Errorf("logs are not available for runtime.type=static")
	}
	if err := validateRemoteOptions(opts); err != nil {
		return err
	}
	command := renderRemoteLogsCommand(opts, component, follow, tail)
	return runRemoteCommandStream(opts, command)
}

func LogsRemoteJSON(opts RemoteOptions, component string, follow bool, tail int, w io.Writer) error {
	if isStaticRuntime(opts.RuntimeType) {
		return fmt.Errorf("logs are not available for runtime.type=static")
	}
	if err := validateRemoteOptions(opts); err != nil {
		return err
	}
	command := renderRemoteLogsCommand(opts, component, follow, tail)
	args := buildSSHArgs(opts, false)
	args = append(args, fmt.Sprintf("%s@%s", opts.RemoteUser, opts.RemoteHost), "bash -lc "+shellQuote(command))

	cmd := exec.Command("ssh", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	type logEvent struct {
		Type   string `json:"type"`
		Stream string `json:"stream"`
		Line   string `json:"line"`
	}
	type scanResult struct {
		stream string
		line   string
		err    error
		done   bool
	}

	results := make(chan scanResult, 64)
	scan := func(stream string, r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			results <- scanResult{stream: stream, line: scanner.Text()}
		}
		results <- scanResult{stream: stream, err: scanner.Err(), done: true}
	}
	go scan("stdout", stdout)
	go scan("stderr", stderr)

	encoder := json.NewEncoder(w)
	doneStreams := 0
	for doneStreams < 2 {
		result := <-results
		if result.done {
			doneStreams++
			if result.err != nil {
				_ = cmd.Wait()
				return result.err
			}
			continue
		}
		if err := encoder.Encode(logEvent{
			Type:   "log",
			Stream: result.stream,
			Line:   result.line,
		}); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}

	return cmd.Wait()
}

func DestroyRemote(opts RemoteOptions, dropDatabase bool) error {
	if err := validateRemoteOptions(opts); err != nil {
		return err
	}
	return runRemoteScript(opts, renderDestroyRemoteScript(opts, dropDatabase))
}

func RollbackRemote(opts RemoteOptions, releaseID string) (ReleaseRecord, error) {
	if err := validateRemoteOptions(opts); err != nil {
		return ReleaseRecord{}, err
	}

	records, err := FetchRemoteReleaseHistory(opts)
	if err != nil {
		return ReleaseRecord{}, err
	}
	if len(records) == 0 {
		return ReleaseRecord{}, fmt.Errorf("no remote release history found")
	}

	target, err := selectRollbackRelease(records, releaseID)
	if err != nil {
		return ReleaseRecord{}, err
	}

	if err := runRemoteScript(opts, renderRollbackRemoteScript(opts, target)); err != nil {
		return ReleaseRecord{}, err
	}
	return target, nil
}

func FetchRemoteReleaseHistory(opts RemoteOptions) ([]ReleaseRecord, error) {
	command := fmt.Sprintf("if [ -f %s ]; then cat %s; fi", shellQuote(releaseHistoryPath(opts)), shellQuote(releaseHistoryPath(opts)))
	out, err := runRemoteCommandCapture(opts, true, command)
	if err != nil {
		return nil, err
	}
	return parseReleaseHistory(out)
}

func selectRollbackRelease(records []ReleaseRecord, releaseID string) (ReleaseRecord, error) {
	if releaseID != "" {
		for i := len(records) - 1; i >= 0; i-- {
			if records[i].ID == releaseID {
				return records[i], nil
			}
		}
		return ReleaseRecord{}, fmt.Errorf("release %s not found in remote history", releaseID)
	}

	current := records[len(records)-1].ID
	for i := len(records) - 2; i >= 0; i-- {
		if records[i].ID != current {
			return records[i], nil
		}
	}

	return ReleaseRecord{}, fmt.Errorf("no previous release available to roll back to")
}

func runRemoteCommandStream(opts RemoteOptions, command string) error {
	args := buildSSHArgs(opts, false)
	args = append(args, fmt.Sprintf("%s@%s", opts.RemoteUser, opts.RemoteHost), "bash -lc "+shellQuote(command))

	cmd := exec.Command("ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func renderRemoteLogsCommand(opts RemoteOptions, component string, follow bool, tail int) string {
	if tail <= 0 {
		tail = 200
	}

	var containerExpr string
	switch strings.TrimSpace(strings.ToLower(component)) {
	case "", "app":
		containerExpr = renderActiveAppContainerExpression(opts)
	case "proxy":
		containerExpr = shellQuote(opts.ProxyContainerName)
	case "postgres":
		containerExpr = shellQuote(sharedPostgresContainer)
	default:
		containerExpr = shellQuote(component)
	}

	args := []string{"docker logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, "--tail", strconv.Itoa(tail), "\"$container_name\"")

	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("container_name=%s", containerExpr),
		"if [ -z \"$container_name\" ]; then",
		"  echo 'no matching container found' >&2",
		"  exit 1",
		"fi",
		"if ! docker ps -a --format '{{.Names}}' | grep -Fx -- \"$container_name\" >/dev/null 2>&1; then",
		"  echo \"container not found: $container_name\" >&2",
		"  exit 1",
		"fi",
		strings.Join(args, " "),
	}
	return strings.Join(lines, "\n")
}

func renderActiveAppContainerExpression(opts RemoteOptions) string {
	activeFile := activeSlotPath(opts)
	base := opts.AppContainerName
	return strings.Join([]string{
		"$(candidate='';",
		"if [ -f " + shellQuote(activeFile) + " ]; then",
		"  slot=$(tr -d '\\r\\n' < " + shellQuote(activeFile) + ");",
		"  if [ -n \"$slot\" ]; then candidate=" + shellQuote(base) + "-$slot; fi;",
		"fi;",
		"if [ -n \"$candidate\" ] && docker ps -a --format '{{.Names}}' | grep -Fx -- \"$candidate\" >/dev/null 2>&1; then",
		"  printf '%s' \"$candidate\";",
		"else",
		"  for candidate in " + shellQuote(base+"-a") + " " + shellQuote(base+"-b") + " " + shellQuote(base) + "; do",
		"    if docker ps -a --format '{{.Names}}' | grep -Fx -- \"$candidate\" >/dev/null 2>&1; then",
		"      printf '%s' \"$candidate\";",
		"      break;",
		"    fi;",
		"  done;",
		"fi)",
	}, " ")
}

func renderDestroyRemoteScript(opts RemoteOptions, dropDatabase bool) string {
	if isStaticRuntime(opts.RuntimeType) {
		return renderDestroyStaticRemoteScript(opts, dropDatabase)
	}
	proxySitesDir := filepathJoinSlash(sharedProxyRoot(opts.RemoteServerPath), "sites")
	siteConfigPath := filepathJoinSlash(proxySitesDir, opts.SiteConfigName)
	containers := []string{
		releaseSlotContainerName(opts.AppContainerName, "a"),
		releaseSlotContainerName(opts.AppContainerName, "b"),
		opts.AppContainerName,
	}
	lines := []string{
		"set -euo pipefail",
	}
	for _, container := range containers {
		lines = append(lines, fmt.Sprintf("docker rm -f %s >/dev/null 2>&1 || true", shellQuote(container)))
	}

	if dropDatabase && opts.SharedDatabase != nil {
		lines = append(lines, renderSharedDatabaseDestroy(opts)...)
	}

	lines = append(lines,
		fmt.Sprintf("rm -f %s %s %s", shellQuote(siteConfigPath), shellQuote(activeSlotPath(opts)), shellQuote(currentReleasePath(opts))),
		fmt.Sprintf("rm -f %s || true", shellQuote(releaseHistoryPath(opts))),
		fmt.Sprintf("rm -rf %s || true", shellQuote(opts.RemoteAppPath)),
		fmt.Sprintf("docker images --format '{{.Repository}}:{{.Tag}}' | grep -F %s | while read -r image; do docker image rm -f \"$image\" >/dev/null 2>&1 || true; done || true",
			shellQuote(releaseImageRepository(opts.ImageTag)+":")),
		fmt.Sprintf("if docker ps --format '{{.Names}}' | grep -Fx -- %s >/dev/null 2>&1; then", shellQuote(opts.ProxyContainerName)),
		fmt.Sprintf("  if find %s -maxdepth 1 -type f -name '*.caddy' | grep -q .; then", shellQuote(proxySitesDir)),
		fmt.Sprintf("    docker exec %s caddy reload --config /etc/caddy/Caddyfile >/dev/null", shellQuote(opts.ProxyContainerName)),
		"  else",
		fmt.Sprintf("    docker rm -f %s >/dev/null 2>&1 || true", shellQuote(opts.ProxyContainerName)),
		"  fi",
		"fi",
	)
	return strings.Join(lines, "\n")
}

func renderDestroyStaticRemoteScript(opts RemoteOptions, dropDatabase bool) string {
	proxySitesDir := filepathJoinSlash(sharedProxyRoot(opts.RemoteServerPath), "sites")
	siteConfigPath := filepathJoinSlash(proxySitesDir, opts.SiteConfigName)
	lines := []string{
		"set -euo pipefail",
	}

	if dropDatabase && opts.SharedDatabase != nil {
		lines = append(lines, renderSharedDatabaseDestroy(opts)...)
	}

	lines = append(lines,
		fmt.Sprintf("rm -f %s %s %s || true", shellQuote(siteConfigPath), shellQuote(currentReleasePath(opts)), shellQuote(activeSlotPath(opts))),
		fmt.Sprintf("rm -f %s || true", shellQuote(releaseHistoryPath(opts))),
		fmt.Sprintf("rm -rf %s || true", shellQuote(opts.RemoteAppPath)),
		fmt.Sprintf("if docker ps --format '{{.Names}}' | grep -Fx -- %s >/dev/null 2>&1; then", shellQuote(opts.ProxyContainerName)),
		fmt.Sprintf("  if find %s -maxdepth 1 -type f -name '*.caddy' | grep -q .; then", shellQuote(proxySitesDir)),
		fmt.Sprintf("    docker exec %s caddy reload --config /etc/caddy/Caddyfile >/dev/null", shellQuote(opts.ProxyContainerName)),
		"  else",
		fmt.Sprintf("    docker rm -f %s >/dev/null 2>&1 || true", shellQuote(opts.ProxyContainerName)),
		"  fi",
		"fi",
	)

	return strings.Join(lines, "\n")
}

func renderSharedDatabaseDestroy(opts RemoteOptions) []string {
	sql := renderSharedDatabaseDestroySQL(*opts.SharedDatabase)
	return []string{
		fmt.Sprintf("if docker ps -a --format '{{.Names}}' | grep -Fx -- %s >/dev/null 2>&1; then", shellQuote(sharedPostgresContainer)),
		fmt.Sprintf("  docker exec -i --user postgres %s psql -v ON_ERROR_STOP=1 -d postgres <<'SQL'\n%s\nSQL", shellQuote(sharedPostgresContainer), sql),
		"fi",
	}
}

func renderSharedDatabaseDestroySQL(opts SharedDatabaseOptions) string {
	dbNameLiteral := sqlQuoteLiteral(opts.Name)
	return strings.Join([]string{
		fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM PUBLIC;", sqlQuoteIdentifier(opts.Name)),
		fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid();", dbNameLiteral),
		fmt.Sprintf("DROP DATABASE IF EXISTS %s;", sqlQuoteIdentifier(opts.Name)),
		fmt.Sprintf("DROP ROLE IF EXISTS %s;", sqlQuoteIdentifier(opts.User)),
	}, "\n")
}

func renderRollbackRemoteScript(opts RemoteOptions, target ReleaseRecord) string {
	if isStaticRuntime(opts.RuntimeType) {
		return renderRollbackStaticRemoteScript(opts, target)
	}
	healthPath := normalizedHealthcheckPath(opts.HealthcheckPath)
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	proxySitesDir := filepath.ToSlash(filepath.Join(proxyRoot, "sites"))
	rootCaddyPath := filepath.ToSlash(filepath.Join(proxyRoot, "Caddyfile"))
	proxyDataPath := filepath.ToSlash(filepath.Join(proxyRoot, "data"))
	proxyConfigPath := filepath.ToSlash(filepath.Join(proxyRoot, "config"))
	siteConfigPath := filepath.ToSlash(filepath.Join(proxySitesDir, opts.SiteConfigName))
	primaryPort, secondaryPort := releaseSlotPorts(opts.ServicePort)
	nextSiteCaddy := renderSiteCaddyfileWithUpstream(opts.Hostnames, opts.RoutePath, "127.0.0.1:${TARGET_PORT}", opts.AnalyticsLogName)
	cleanup := renderReleaseCleanupCommands(opts, "history_limit")

	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("TARGET_IMAGE=%s", shellQuote(target.Image)),
		fmt.Sprintf("TARGET_RELEASE=%s", shellQuote(target.ID)),
		fmt.Sprintf("TARGET_SHA=%s", shellQuote(target.ArtifactSHA)),
		fmt.Sprintf("PRIMARY_PORT=%d", primaryPort),
		fmt.Sprintf("SECONDARY_PORT=%d", secondaryPort),
		fmt.Sprintf("ACTIVE_SLOT_FILE=%s", shellQuote(activeSlotPath(opts))),
		fmt.Sprintf("CURRENT_RELEASE_FILE=%s", shellQuote(currentReleasePath(opts))),
		fmt.Sprintf("HISTORY_FILE=%s", shellQuote(releaseHistoryPath(opts))),
		fmt.Sprintf("APP_CONTAINER_BASE=%s", shellQuote(opts.AppContainerName)),
		"active_slot=''",
		`if [ -f "$ACTIVE_SLOT_FILE" ]; then active_slot="$(tr -d '\r\n' < "$ACTIVE_SLOT_FILE")"; fi`,
		"if [ \"$active_slot\" = 'a' ]; then",
		"  next_slot='b'",
		"  next_port=$SECONDARY_PORT",
		"  old_slot='a'",
		"else",
		"  next_slot='a'",
		"  next_port=$PRIMARY_PORT",
		"  old_slot='b'",
		"fi",
		"next_container=\"$APP_CONTAINER_BASE-$next_slot\"",
		"old_container=\"$APP_CONTAINER_BASE-$old_slot\"",
		"HEALTHCHECK_PATH=" + shellQuote(healthPath),
		"docker image inspect \"$TARGET_IMAGE\" >/dev/null 2>&1",
		"docker rm -f \"$next_container\" >/dev/null 2>&1 || true",
		fmt.Sprintf("docker run -d --restart unless-stopped --network %s --env-file %s --name \"$next_container\" -p 127.0.0.1:${next_port}:%d \"$TARGET_IMAGE\" >/dev/null",
			shellQuote(sharedDockerNetwork), shellQuote(filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "app.runtime.env"))), opts.ContainerPort),
		"attempt=0",
		"until docker run --rm --network host curlimages/curl:8.12.1 -fsS \"http://127.0.0.1:${next_port}${HEALTHCHECK_PATH}\" >/dev/null 2>&1; do",
		"  attempt=$((attempt + 1))",
		"  if [ \"$attempt\" -ge 30 ]; then",
		"    docker logs \"$next_container\" || true",
		"    exit 1",
		"  fi",
		"  sleep 2",
		"done",
		"TARGET_PORT=\"$next_port\"",
	}
	lines = append(lines, renderMaintenanceAwareSiteConfigCommands(opts, siteConfigPath, nextSiteCaddy)...)
	lines = append(lines, renderProxyReloadCommands(opts, rootCaddyPath, proxySitesDir, proxyDataPath, proxyConfigPath)...)
	lines = append(lines,
		"if docker ps --format '{{.Names}}' | grep -Fx -- \"$old_container\" >/dev/null 2>&1; then",
		"  docker rm -f \"$old_container\" >/dev/null 2>&1 || true",
		"fi",
		"printf '%s\\n' \"$next_slot\" > \"$ACTIVE_SLOT_FILE\"",
		"printf '%s\\n' \"$TARGET_RELEASE\" > \"$CURRENT_RELEASE_FILE\"",
		"printf '%s\\t%s\\t%s\\t%s\\t%s\\t%s\\n' \"$TARGET_RELEASE\" \"$next_slot\" \"$next_port\" \"$TARGET_IMAGE\" \"$TARGET_SHA\" \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\" >> \"$HISTORY_FILE\"",
		fmt.Sprintf("history_limit=%d", releaseKeepCount(opts)),
		"if [ -f \"$HISTORY_FILE\" ]; then tail -n \"$history_limit\" \"$HISTORY_FILE\" > \"$HISTORY_FILE.tmp\" && mv \"$HISTORY_FILE.tmp\" \"$HISTORY_FILE\"; fi",
		cleanup,
	)
	return strings.Join(lines, "\n")
}

func renderRollbackStaticRemoteScript(opts RemoteOptions, target ReleaseRecord) string {
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	proxySitesDir := filepath.ToSlash(filepath.Join(proxyRoot, "sites"))
	rootCaddyPath := filepath.ToSlash(filepath.Join(proxyRoot, "Caddyfile"))
	proxyDataPath := filepath.ToSlash(filepath.Join(proxyRoot, "data"))
	proxyConfigPath := filepath.ToSlash(filepath.Join(proxyRoot, "config"))
	siteConfigPath := filepath.ToSlash(filepath.Join(proxySitesDir, opts.SiteConfigName))
	targetRoot := staticReleaseRootPath(opts, target.ID)
	staticRootPath := staticCurrentRootPath(opts)
	siteCaddy := renderStaticSiteCaddyfile(opts.Hostnames, opts.RoutePath, staticRootPath, opts.AnalyticsLogName)
	cleanup := renderReleaseCleanupCommands(opts, "history_limit")

	lines := []string{
		"set -euo pipefail",
		fmt.Sprintf("TARGET_ROOT=%s", shellQuote(targetRoot)),
		fmt.Sprintf("TARGET_RELEASE=%s", shellQuote(target.ID)),
		fmt.Sprintf("TARGET_SHA=%s", shellQuote(target.ArtifactSHA)),
		fmt.Sprintf("CURRENT_RELEASE_FILE=%s", shellQuote(currentReleasePath(opts))),
		fmt.Sprintf("HISTORY_FILE=%s", shellQuote(releaseHistoryPath(opts))),
		"if [ ! -d \"$TARGET_ROOT\" ]; then",
		"  echo 'target static release is missing on the remote host' >&2",
		"  exit 1",
		"fi",
		fmt.Sprintf("rm -rf %s", shellQuote(staticRootPath)),
		fmt.Sprintf("ln -sfn \"$TARGET_ROOT\" %s", shellQuote(staticRootPath)),
		fmt.Sprintf("cat > %s <<'EOF'\n%sEOF", shellQuote(rootCaddyPath), renderRootCaddyfile()),
	}
	lines = append(lines, renderMaintenanceAwareSiteConfigCommands(opts, siteConfigPath, siteCaddy)...)
	lines = append(lines, renderProxyReloadCommands(opts, rootCaddyPath, proxySitesDir, proxyDataPath, proxyConfigPath)...)
	lines = append(lines,
		"printf '%s\\n' \"$TARGET_RELEASE\" > \"$CURRENT_RELEASE_FILE\"",
		fmt.Sprintf("printf '%%s\\t%%s\\t%%s\\t%%s\\t%%s\\t%%s\\n' \"$TARGET_RELEASE\" 'static' '0' %s \"$TARGET_SHA\" \"$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)\" >> \"$HISTORY_FILE\"",
			shellQuote(staticReleaseMarker(target.ID))),
		fmt.Sprintf("history_limit=%d", releaseKeepCount(opts)),
		"if [ -f \"$HISTORY_FILE\" ]; then tail -n \"$history_limit\" \"$HISTORY_FILE\" > \"$HISTORY_FILE.tmp\" && mv \"$HISTORY_FILE.tmp\" \"$HISTORY_FILE\"; fi",
		cleanup,
	)

	return strings.Join(lines, "\n")
}
