package deploy

import (
	"fmt"
	"strings"
)

// EdgeOptions is self-hosted request hardening rendered into a project's
// proxy route fragments. The zero value renders nothing, so routes without
// edge settings produce exactly the same Caddy config as before.
type EdgeOptions struct {
	SecurityHeaders bool
	CacheStatic     bool
	BlockProbes     bool
	MaxBodySize     string
}

// Enabled reports whether any edge setting is on.
func (e EdgeOptions) Enabled() bool {
	return e.SecurityHeaders || e.CacheStatic || e.BlockProbes || strings.TrimSpace(e.MaxBodySize) != ""
}

// probePaths are exploit-scanner targets that a Node web app never serves.
// Leading "*" makes them match under any route prefix (Caddy path matchers
// are case-insensitive).
var probePaths = []string{
	"*/.env",
	"*/.env.*",
	"*/.git/*",
	"*/.aws/*",
	"*/wp-admin*",
	"*/wp-login.php",
	"*/xmlrpc.php",
	"*/phpmyadmin*",
}

// staticAssetPaths are content-hashed build outputs, safe to cache for a
// year because a changed file gets a new name. Generic folders such as
// /assets/ are left out on purpose: they can hold unhashed files.
var staticAssetPaths = []string{
	"*/_next/static/*",   // Next.js
	"*/_build/assets/*",  // SolidStart / Vinxi
	"*/_nuxt/*",          // Nuxt
	"*/_app/immutable/*", // SvelteKit
	"*/_astro/*",         // Astro
}

// renderEdgeDirectives returns the Caddy directives for a route block. Caddy
// orders directives inside a handle block itself (request_body, header and
// respond all run before reverse_proxy), so their order here is cosmetic.
// No line may contain "$" or a backtick: fragments are written through an
// unquoted shell heredoc.
func renderEdgeDirectives(edge EdgeOptions, indent string) []string {
	if !edge.Enabled() {
		return nil
	}
	var lines []string
	if size := strings.TrimSpace(edge.MaxBodySize); size != "" {
		lines = append(lines,
			indent+"request_body {",
			fmt.Sprintf("%s  max_size %s", indent, size),
			indent+"}",
		)
	}
	// Inline path matchers only work when the path starts with "/", so the
	// suffix/substring patterns below need named matchers. Matcher names are
	// scoped to the handle block they are defined in, so fragments from
	// different projects on one hostname do not collide.
	if edge.BlockProbes {
		lines = append(lines,
			fmt.Sprintf("%s@eu_edge_probe path %s", indent, strings.Join(probePaths, " ")),
			indent+"respond @eu_edge_probe 404",
		)
	}
	if edge.SecurityHeaders {
		// "?" only sets a header the app did not set itself.
		lines = append(lines,
			indent+"header {",
			indent+`  ?Strict-Transport-Security "max-age=31536000"`,
			indent+`  ?X-Content-Type-Options "nosniff"`,
			indent+`  ?X-Frame-Options "SAMEORIGIN"`,
			indent+`  ?Referrer-Policy "strict-origin-when-cross-origin"`,
			indent+"  -Server",
			indent+"  defer",
			indent+"}",
		)
	}
	if edge.CacheStatic {
		lines = append(lines,
			fmt.Sprintf("%s@eu_edge_static path %s", indent, strings.Join(staticAssetPaths, " ")),
			indent+`header @eu_edge_static ?Cache-Control "public, max-age=31536000, immutable"`,
		)
	}
	return lines
}
