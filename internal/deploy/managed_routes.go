package deploy

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func renderManagedRouteConfigCommands(opts RemoteOptions, upstream string) []string {
	if len(opts.Routes) == 0 {
		return nil
	}
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	routesRoot := filepath.ToSlash(filepath.Join(proxyRoot, "routes"))
	sitesRoot := filepath.ToSlash(filepath.Join(proxyRoot, "sites"))
	marker := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, ".route-fragments"))
	affected := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, ".route-hosts.tmp"))
	project := SanitizeDockerName(opts.ProjectName)
	if project == "" {
		project = "app"
	}

	lines := []string{
		fmt.Sprintf("mkdir -p %s", shellQuote(routesRoot)),
		fmt.Sprintf(": > %s", shellQuote(affected)),
		fmt.Sprintf("if [ -f %s ]; then", shellQuote(marker)),
		"  while IFS='|' read -r host safe fragment; do",
		"    [ -z \"$fragment\" ] && continue",
		"    rm -f \"$fragment\"",
		"    rm -f \"$fragment.log\"",
		fmt.Sprintf("    printf '%%s|%%s\\n' \"$host\" \"$safe\" >> %s", shellQuote(affected)),
		fmt.Sprintf("  done < %s", shellQuote(marker)),
		"fi",
		fmt.Sprintf(": > %s", shellQuote(marker)),
	}

	bindings := flattenRemoteRoutes(opts.Routes)
	for index, binding := range bindings {
		safeHost := strings.TrimSuffix(BuildHetznerSiteConfigName(binding.hostname), ".caddy")
		routeDir := filepath.ToSlash(filepath.Join(routesRoot, safeHost))
		fragment := filepath.ToSlash(filepath.Join(routeDir, managedRouteFragmentName(project, binding.route, index)))
		content := renderManagedRouteFragment(binding.route, upstream)
		maintenanceContent := renderManagedMaintenanceRouteFragment(binding.route, maintenanceRootPath(opts))
		statePath := maintenanceStatePath(opts)
		lines = append(lines, fmt.Sprintf("mkdir -p %s", shellQuote(routeDir)))
		extra := strings.TrimSpace(binding.route.CaddyExtra)
		lines = append(lines,
			fmt.Sprintf("if [ -f %s ] && grep -Eq '\"enabled\"[[:space:]]*:[[:space:]]*true' %s; then", shellQuote(statePath), shellQuote(statePath)),
			fmt.Sprintf("  cat > %s <<'EUDEPLOY_MAINTENANCE_ROUTE'\n%sEUDEPLOY_MAINTENANCE_ROUTE", shellQuote(fragment), maintenanceContent),
			"else",
		)
		if extra != "" {
			lines = append(lines,
				fmt.Sprintf("  cat > %s <<'EUDEPLOY_CADDY_EXTRA'\n%s\nEUDEPLOY_CADDY_EXTRA", shellQuote(fragment), extra),
				fmt.Sprintf("  cat >> %s <<EOF\n%sEOF", shellQuote(fragment), content),
			)
		} else {
			lines = append(lines, fmt.Sprintf("  cat > %s <<EOF\n%sEOF", shellQuote(fragment), content))
		}
		lines = append(lines, "fi")
		lines = append(lines,
			fmt.Sprintf("printf '%%s\\n' %s > %s", shellQuote(opts.AnalyticsLogName), shellQuote(fragment+".log")),
			fmt.Sprintf("printf '%%s|%%s|%%s\\n' %s %s %s >> %s", shellQuote(binding.hostname), shellQuote(safeHost), shellQuote(fragment), shellQuote(marker)),
			fmt.Sprintf("printf '%%s|%%s\\n' %s %s >> %s", shellQuote(binding.hostname), shellQuote(safeHost), shellQuote(affected)),
		)
	}

	lines = append(lines,
		fmt.Sprintf("sort -u %s -o %s", shellQuote(affected), shellQuote(affected)),
		"while IFS='|' read -r host safe; do",
		"  [ -z \"$safe\" ] && continue",
		fmt.Sprintf("  route_dir=%s/\"$safe\"", shellQuote(routesRoot)),
		fmt.Sprintf("  site_path=%s/\"$safe.caddy\"", shellQuote(sitesRoot)),
		"  if find \"$route_dir\" -maxdepth 1 -type f -name '*.caddy' -print -quit 2>/dev/null | grep -q .; then",
		"    host_label=\"$host\"",
		"    if printf '%s' \"$host\" | grep -Eq '^([0-9]{1,3}\\.){3}[0-9]{1,3}$'; then host_label=\"http://$host\"; fi",
		"    {",
		"      printf '%s {\\n' \"$host_label\"",
		"      printf '  encode zstd gzip\\n'",
		"      last_fragment=\"$(find \"$route_dir\" -maxdepth 1 -type f -name '*.caddy' | sort | tail -1)\"",
		"      log_name=\"$safe\"",
		"      [ -f \"$last_fragment.log\" ] && log_name=\"$(tr -d '\\r\\n' < \"$last_fragment.log\")\"",
		"      printf '  log {\\n    output file /var/log/caddy/%s.access.log {\\n      roll_size 50MiB\\n      roll_keep 5\\n    }\\n    format json\\n  }\\n' \"$log_name\"",
		"      printf '  route {\\n'",
		fmt.Sprintf("      printf '    import %s/%%s/*.caddy\\n' \"$safe\"", routesRoot),
		"      printf '    respond 404\\n  }\\n}\\n'",
		"    } > \"$site_path\"",
		"  else",
		"    rm -f \"$site_path\"",
		"    rmdir \"$route_dir\" 2>/dev/null || true",
		"  fi",
		fmt.Sprintf("done < %s", shellQuote(affected)),
		fmt.Sprintf("rm -f %s", shellQuote(affected)),
	)
	return lines
}

func renderManagedMaintenanceRouteFragment(route RemoteRoute, rootPath string) string {
	path := normalizeRoutePath(route.Path)
	if path == "/" {
		return strings.Join([]string{
			"handle {",
			`  header Cache-Control "no-store"`,
			fmt.Sprintf("  root * %s", rootPath),
			"  try_files {path} /index.html",
			"  file_server",
			"}",
			"",
		}, "\n")
	}
	return strings.Join([]string{
		fmt.Sprintf("handle_path %s* {", path),
		`  header Cache-Control "no-store"`,
		fmt.Sprintf("  root * %s", rootPath),
		"  try_files {path} /index.html",
		"  file_server",
		"}",
		"",
	}, "\n")
}

func renderManagedRouteRemovalCommands(opts RemoteOptions) []string {
	if len(opts.Routes) == 0 {
		return nil
	}
	proxyRoot := sharedProxyRoot(opts.RemoteServerPath)
	routesRoot := filepath.ToSlash(filepath.Join(proxyRoot, "routes"))
	sitesRoot := filepath.ToSlash(filepath.Join(proxyRoot, "sites"))
	marker := filepath.ToSlash(filepath.Join(opts.RemoteAppPath, ".route-fragments"))
	return []string{
		fmt.Sprintf("if [ -f %s ]; then", shellQuote(marker)),
		"  while IFS='|' read -r host safe fragment; do",
		"    [ -z \"$fragment\" ] && continue",
		"    rm -f \"$fragment\"",
		"    rm -f \"$fragment.log\"",
		fmt.Sprintf("    route_dir=%s/\"$safe\"", shellQuote(routesRoot)),
		fmt.Sprintf("    site_path=%s/\"$safe.caddy\"", shellQuote(sitesRoot)),
		"    if ! find \"$route_dir\" -maxdepth 1 -type f -name '*.caddy' -print -quit 2>/dev/null | grep -q .; then",
		"      rm -f \"$site_path\"",
		"      rmdir \"$route_dir\" 2>/dev/null || true",
		"    fi",
		"  done < " + shellQuote(marker),
		"fi",
	}
}

type flattenedRemoteRoute struct {
	hostname string
	route    RemoteRoute
}

func flattenRemoteRoutes(routes []RemoteRoute) []flattenedRemoteRoute {
	result := make([]flattenedRemoteRoute, 0)
	for _, route := range routes {
		for _, hostname := range route.Hostnames {
			hostname = strings.TrimSpace(hostname)
			if hostname != "" {
				result = append(result, flattenedRemoteRoute{hostname: hostname, route: route})
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := normalizeRoutePath(result[i].route.Path)
		right := normalizeRoutePath(result[j].route.Path)
		if result[i].hostname != result[j].hostname {
			return result[i].hostname < result[j].hostname
		}
		if left == "/" {
			return false
		}
		if right == "/" {
			return true
		}
		return len(left) > len(right)
	})
	return result
}

func managedRouteFragmentName(project string, route RemoteRoute, index int) string {
	path := normalizeRoutePath(route.Path)
	priority := 999999
	if path != "/" {
		priority -= len(path)
	}
	return fmt.Sprintf("%06d-%s-%03d.caddy", priority, project, index)
}

func renderManagedRouteFragment(route RemoteRoute, upstream string) string {
	if route.Redirect != "" {
		code := route.Code
		if code == 0 {
			code = 301
		}
		return fmt.Sprintf("redir %s %d\n", route.Redirect, code)
	}
	return strings.Join(renderCaddyRoute(CaddyRoute{
		Path:           route.Path,
		Upstream:       upstream,
		PreservePrefix: route.PreservePrefix,
	}, ""), "\n") + "\n"
}
