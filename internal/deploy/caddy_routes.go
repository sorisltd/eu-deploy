package deploy

import (
	"fmt"
	"sort"
	"strings"
)

type CaddyRoute struct {
	Hostnames      []string
	Path           string
	Upstream       string
	PreservePrefix bool
	Redirect       string
	Code           int
}

func RenderManagedCaddyfile(routes []CaddyRoute) string {
	byHost := map[string][]CaddyRoute{}
	for _, route := range routes {
		for _, hostname := range route.Hostnames {
			hostname = strings.TrimSpace(hostname)
			if hostname != "" {
				byHost[hostname] = append(byHost[hostname], route)
			}
		}
	}

	type group struct {
		fingerprint string
		hostnames   []string
		routes      []CaddyRoute
	}
	groupsByFingerprint := map[string]*group{}
	for hostname, hostRoutes := range byHost {
		sortCaddyRoutes(hostRoutes)
		fingerprint := caddyRouteFingerprint(hostRoutes)
		entry := groupsByFingerprint[fingerprint]
		if entry == nil {
			entry = &group{fingerprint: fingerprint, routes: hostRoutes}
			groupsByFingerprint[fingerprint] = entry
		}
		entry.hostnames = append(entry.hostnames, hostname)
	}

	groups := make([]*group, 0, len(groupsByFingerprint))
	for _, entry := range groupsByFingerprint {
		sort.Strings(entry.hostnames)
		groups = append(groups, entry)
	}
	sort.Slice(groups, func(i, j int) bool {
		return strings.Join(groups[i].hostnames, ",") < strings.Join(groups[j].hostnames, ",")
	})

	blocks := make([]string, 0, len(groups))
	for _, entry := range groups {
		lines := []string{fmt.Sprintf("%s {", formatCaddySiteHosts(entry.hostnames))}
		for _, route := range entry.routes {
			lines = append(lines, renderCaddyRoute(route, "  ")...)
		}
		lines = append(lines, "}")
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

func sortCaddyRoutes(routes []CaddyRoute) {
	sort.SliceStable(routes, func(i, j int) bool {
		left := normalizeRoutePath(routes[i].Path)
		right := normalizeRoutePath(routes[j].Path)
		if left == "/" && right != "/" {
			return false
		}
		if left != "/" && right == "/" {
			return true
		}
		if len(left) != len(right) {
			return len(left) > len(right)
		}
		return left < right
	})
}

func caddyRouteFingerprint(routes []CaddyRoute) string {
	parts := make([]string, 0, len(routes))
	for _, route := range routes {
		parts = append(parts, fmt.Sprintf("%s|%s|%t|%s|%d", normalizeRoutePath(route.Path), route.Upstream, route.PreservePrefix, route.Redirect, route.Code))
	}
	return strings.Join(parts, "\n")
}

func renderCaddyRoute(route CaddyRoute, indent string) []string {
	if route.Redirect != "" {
		code := route.Code
		if code == 0 {
			code = 301
		}
		return []string{fmt.Sprintf("%sredir %s %d", indent, route.Redirect, code)}
	}

	path := normalizeRoutePath(route.Path)
	if path == "/" {
		return []string{
			indent + "handle {",
			fmt.Sprintf("%s  reverse_proxy %s", indent, strings.TrimSpace(route.Upstream)),
			indent + "}",
		}
	}
	directive := "handle_path"
	if route.PreservePrefix {
		directive = "handle"
	}
	return []string{
		fmt.Sprintf("%s%s %s* {", indent, directive, path),
		fmt.Sprintf("%s  reverse_proxy %s", indent, strings.TrimSpace(route.Upstream)),
		indent + "}",
	}
}
