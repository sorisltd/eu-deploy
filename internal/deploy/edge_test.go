package deploy

import (
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var updateEdgeGolden = flag.Bool("update-edge-golden", false, "rewrite edge golden files")

var fullEdge = EdgeOptions{SecurityHeaders: true, CacheStatic: true, BlockProbes: true, MaxBodySize: "10MB"}

// Routes without edge settings must render byte-for-byte what they rendered
// before edge existed, so existing deploys see no proxy change.
func TestEdgeZeroValueKeepsLegacyFragments(t *testing.T) {
	cases := map[string]struct {
		route RemoteRoute
		want  string
	}{
		"root":            {RemoteRoute{Path: "/"}, "handle {\n  reverse_proxy 127.0.0.1:3010\n}\n"},
		"prefix":          {RemoteRoute{Path: "/api"}, "handle_path /api* {\n  reverse_proxy 127.0.0.1:3010\n}\n"},
		"preserve prefix": {RemoteRoute{Path: "/darbai", PreservePrefix: true}, "handle /darbai* {\n  reverse_proxy 127.0.0.1:3010\n}\n"},
		"redirect":        {RemoteRoute{Redirect: "https://bustora.lt/meistrams", Code: 301}, "redir https://bustora.lt/meistrams 301\n"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := renderManagedRouteFragment(test.route, "127.0.0.1:3010"); got != test.want {
				t.Fatalf("fragment changed:\n got: %q\nwant: %q", got, test.want)
			}
		})
	}
}

func TestEdgeRedirectRoutesIgnoreEdge(t *testing.T) {
	route := RemoteRoute{Redirect: "https://bustora.lt/meistrams", Code: 308, Edge: fullEdge}
	if got := renderManagedRouteFragment(route, "127.0.0.1:3010"); got != "redir https://bustora.lt/meistrams 308\n" {
		t.Fatalf("redirect fragment must not carry edge rules: %q", got)
	}
}

func TestEdgeFragmentGolden(t *testing.T) {
	cases := map[string]RemoteRoute{
		"edge-root-route.golden":            {Path: "/", Edge: fullEdge},
		"edge-preserve-prefix-route.golden": {Path: "/darbai", PreservePrefix: true, Edge: EdgeOptions{SecurityHeaders: true, BlockProbes: true}},
	}
	for file, route := range cases {
		t.Run(file, func(t *testing.T) {
			got := renderManagedRouteFragment(route, "127.0.0.1:${TARGET_PORT}")
			path := filepath.Join("testdata", "caddy", file)
			if *updateEdgeGolden {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got != string(want) {
				t.Fatalf("fragment mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// Fragments are written through an unquoted heredoc so ${TARGET_PORT} can
// expand; edge lines must not contain anything else the shell would expand.
func TestEdgeDirectivesAreShellSafe(t *testing.T) {
	for _, line := range renderEdgeDirectives(fullEdge, "  ") {
		if strings.ContainsAny(line, "$`\\") {
			t.Fatalf("edge line would be altered by the shell heredoc: %q", line)
		}
	}
}

// Runs the real deploy-side script and checks the fragment on disk matches
// what was rendered, i.e. the heredoc wrote the edge rules unchanged.
func TestEdgeFragmentWrittenByDeployScript(t *testing.T) {
	serverRoot := t.TempDir()
	appPath := filepath.Join(serverRoot, "apps", "bustora")
	for _, path := range []string{appPath, filepath.Join(serverRoot, "_proxy", "sites")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	route := RemoteRoute{Hostnames: []string{"bustora.lt"}, Path: "/", Edge: fullEdge}
	script := strings.Join(renderManagedRouteConfigCommands(RemoteOptions{
		ProjectName:      "bustora",
		AnalyticsLogName: "bustora",
		RemoteServerPath: serverRoot,
		RemoteAppPath:    appPath,
		Routes:           []RemoteRoute{route},
	}, "127.0.0.1:${TARGET_PORT}"), "\n")
	command := exec.Command("bash", "-c", "set -euo pipefail\n"+script)
	command.Env = append(os.Environ(), "TARGET_PORT=3010")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("route script failed: %v\n%s", err, output)
	}
	written, err := os.ReadFile(filepath.Join(serverRoot, "_proxy", "routes", "bustora.lt", "999999-bustora-000.caddy"))
	if err != nil {
		t.Fatalf("read fragment: %v", err)
	}
	if want := renderManagedRouteFragment(route, "127.0.0.1:3010"); string(written) != want {
		t.Fatalf("fragment on disk differs from rendered:\n--- disk ---\n%s\n--- rendered ---\n%s", written, want)
	}
}

// Validates a composed site with a real Caddy binary when one is available
// (caddy on PATH, or EU_CADDY_BIN). Skipped otherwise.
func TestEdgeFragmentsPassCaddyValidate(t *testing.T) {
	caddyBin := os.Getenv("EU_CADDY_BIN")
	if caddyBin == "" {
		found, err := exec.LookPath("caddy")
		if err != nil {
			t.Skip("caddy binary not available; set EU_CADDY_BIN to run")
		}
		caddyBin = found
	}
	serverRoot := t.TempDir()
	sites := filepath.Join(serverRoot, "_proxy", "sites")
	run := func(project string, route RemoteRoute) {
		appPath := filepath.Join(serverRoot, "apps", project)
		for _, path := range []string{appPath, sites} {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", path, err)
			}
		}
		script := strings.Join(renderManagedRouteConfigCommands(RemoteOptions{
			ProjectName:      project,
			AnalyticsLogName: project,
			RemoteServerPath: serverRoot,
			RemoteAppPath:    appPath,
			Routes:           []RemoteRoute{route},
		}, "127.0.0.1:${TARGET_PORT}"), "\n")
		command := exec.Command("bash", "-c", "set -euo pipefail\n"+script)
		command.Env = append(os.Environ(), "TARGET_PORT=3010")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("route script failed: %v\n%s", err, output)
		}
	}
	// Two projects sharing one hostname, as bustora and safebuild do.
	run("bustora", RemoteRoute{Hostnames: []string{"bustora.lt"}, Path: "/", Edge: fullEdge})
	run("safebuild", RemoteRoute{Hostnames: []string{"bustora.lt"}, Path: "/darbai", PreservePrefix: true, Edge: fullEdge})

	logDir := filepath.Join(serverRoot, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	site, err := os.ReadFile(filepath.Join(sites, "bustora.lt.caddy"))
	if err != nil {
		t.Fatalf("read site: %v", err)
	}
	// Point the production log path at a temp dir so validate can open it.
	siteText := strings.ReplaceAll(string(site), "/var/log/caddy/", logDir+"/")
	caddyfile := filepath.Join(serverRoot, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("{\n  auto_https disable_certs\n}\n\n"+siteText), 0o644); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}
	command := exec.Command(caddyBin, "validate", "--adapter", "caddyfile", "--config", caddyfile)
	command.Env = append(os.Environ(), "XDG_DATA_HOME="+serverRoot, "XDG_CONFIG_HOME="+serverRoot, "HOME="+serverRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("caddy validate failed: %v\n%s\n--- Caddyfile ---\n%s", err, output, siteText)
	}
}

// Runs a real Caddy with two projects' edge fragments on one hostname, in
// front of a stand-in app, and checks what requests actually get back.
// Skipped when no caddy binary is available (see EU_CADDY_BIN).
func TestEdgeRulesBehaveUnderRealCaddy(t *testing.T) {
	caddyBin := os.Getenv("EU_CADDY_BIN")
	if caddyBin == "" {
		found, err := exec.LookPath("caddy")
		if err != nil {
			t.Skip("caddy binary not available; set EU_CADDY_BIN to run")
		}
		caddyBin = found
	}

	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/framed":
			w.Header().Set("X-Frame-Options", "DENY")
		case "/_build/assets/nocache.js":
			w.Header().Set("Cache-Control", "no-cache")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "app:"+r.URL.Path)
	}))
	defer app.Close()
	appURL, _ := url.Parse(app.URL)

	serverRoot := t.TempDir()
	sites := filepath.Join(serverRoot, "_proxy", "sites")
	deployRoute := func(project string, route RemoteRoute) {
		appPath := filepath.Join(serverRoot, "apps", project)
		for _, path := range []string{appPath, sites} {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", path, err)
			}
		}
		script := strings.Join(renderManagedRouteConfigCommands(RemoteOptions{
			ProjectName:      project,
			AnalyticsLogName: project,
			RemoteServerPath: serverRoot,
			RemoteAppPath:    appPath,
			Routes:           []RemoteRoute{route},
		}, "127.0.0.1:${TARGET_PORT}"), "\n")
		command := exec.Command("bash", "-c", "set -euo pipefail\n"+script)
		command.Env = append(os.Environ(), "TARGET_PORT="+appURL.Port())
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("route script failed: %v\n%s", err, output)
		}
	}
	deployRoute("bustora", RemoteRoute{Hostnames: []string{"bustora.lt"}, Path: "/", Edge: fullEdge})
	deployRoute("safebuild", RemoteRoute{Hostnames: []string{"bustora.lt"}, Path: "/darbai", PreservePrefix: true, Edge: fullEdge})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	caddyAddr := listener.Addr().String()
	listener.Close()

	logDir := filepath.Join(serverRoot, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	site, err := os.ReadFile(filepath.Join(sites, "bustora.lt.caddy"))
	if err != nil {
		t.Fatalf("read site: %v", err)
	}
	siteText := strings.ReplaceAll(string(site), "/var/log/caddy/", logDir+"/")
	siteText = strings.Replace(siteText, "bustora.lt {", "http://"+caddyAddr+" {", 1)
	caddyfile := filepath.Join(serverRoot, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("{\n  admin off\n}\n\n"+siteText), 0o644); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}
	caddy := exec.Command(caddyBin, "run", "--adapter", "caddyfile", "--config", caddyfile)
	caddy.Env = append(os.Environ(), "XDG_DATA_HOME="+serverRoot, "XDG_CONFIG_HOME="+serverRoot, "HOME="+serverRoot)
	var caddyOutput strings.Builder
	caddy.Stdout = &caddyOutput
	caddy.Stderr = &caddyOutput
	if err := caddy.Start(); err != nil {
		t.Fatalf("start caddy: %v", err)
	}
	defer func() {
		_ = caddy.Process.Kill()
		_ = caddy.Wait()
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + caddyAddr
	ready := false
	for attempt := 0; attempt < 50; attempt++ {
		if response, err := client.Get(base + "/"); err == nil {
			response.Body.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("caddy did not start:\n%s", caddyOutput.String())
	}

	do := func(method, path string, body io.Reader) (*http.Response, string) {
		t.Helper()
		request, err := http.NewRequest(method, base+path, body)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer response.Body.Close()
		text, _ := io.ReadAll(response.Body)
		return response, string(text)
	}

	// Normal traffic reaches the right app, with security headers added.
	response, body := do("GET", "/", nil)
	if response.StatusCode != 200 || body != "app:/" {
		t.Fatalf("GET / = %d %q", response.StatusCode, body)
	}
	for header, want := range map[string]string{
		"Strict-Transport-Security": "max-age=31536000",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "SAMEORIGIN",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
	} {
		if got := response.Header.Get(header); got != want {
			t.Fatalf("GET / header %s = %q, want %q", header, got, want)
		}
	}
	if server := response.Header.Get("Server"); server != "" {
		t.Fatalf("Server header should be removed, got %q", server)
	}
	for _, path := range []string{"/darbai/x", "/environment", "/about/env", "/assets/logo.png"} {
		if response, body := do("GET", path, nil); response.StatusCode != 200 || body != "app:"+path {
			t.Fatalf("GET %s = %d %q, want app response", path, response.StatusCode, body)
		}
	}

	// Scanner paths are stopped before the app, under any prefix and case.
	for _, path := range []string{"/.env", "/.env.local", "/darbai/.env", "/.git/config", "/wp-admin/", "/WP-ADMIN/", "/wp-login.php", "/darbai/xmlrpc.php"} {
		if response, body := do("GET", path, nil); response.StatusCode != 404 || strings.HasPrefix(body, "app:") {
			t.Fatalf("GET %s = %d %q, want 404 from proxy", path, response.StatusCode, body)
		}
	}

	// Hashed build assets get long caching; app-set headers win.
	if response, _ := do("GET", "/_build/assets/app-3f9a.js", nil); response.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("hashed asset Cache-Control = %q", response.Header.Get("Cache-Control"))
	}
	if response, _ := do("GET", "/_build/assets/nocache.js", nil); response.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("app Cache-Control must win, got %q", response.Header.Get("Cache-Control"))
	}
	if response, _ := do("GET", "/assets/logo.png", nil); response.Header.Get("Cache-Control") != "" {
		t.Fatalf("unhashed asset must not get Cache-Control, got %q", response.Header.Get("Cache-Control"))
	}
	if response, _ := do("GET", "/framed", nil); response.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("app X-Frame-Options must win, got %q", response.Header.Get("X-Frame-Options"))
	}

	// Oversized bodies are refused; normal ones pass.
	if response, _ := do("POST", "/", strings.NewReader(strings.Repeat("x", 11<<20))); response.StatusCode != 413 {
		t.Fatalf("11MB POST = %d, want 413", response.StatusCode)
	}
	if response, body := do("POST", "/", strings.NewReader("small")); response.StatusCode != 200 || body != "app:/" {
		t.Fatalf("small POST = %d %q", response.StatusCode, body)
	}
}
