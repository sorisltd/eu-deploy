package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Result struct {
	Framework    string
	ProjectName  string
	RuntimeType  string
	BuildCommand string
	StartCommand string
	OutputDir    string
	Warnings     []string
}

type pkgJSON struct {
	Name    string            `json:"name"`
	Scripts map[string]string `json:"scripts"`
	Deps    map[string]string `json:"dependencies"`
	DevDeps map[string]string `json:"devDependencies"`
}

func Detect(dir string) Result {
	// Defaults (generic Node app)
	res := Result{
		Framework:   "auto",
		ProjectName: filepath.Base(dir),
		RuntimeType: "web",
		OutputDir:   "dist",
	}

	p := filepath.Join(dir, "package.json")
	b, err := os.ReadFile(p)
	if err != nil {
		res.BuildCommand = "npm run build"
		res.StartCommand = "npm run start"
		return res
	}

	var pj pkgJSON
	if err := json.Unmarshal(b, &pj); err != nil {
		res.BuildCommand = "npm run build"
		res.StartCommand = "npm run start"
		return res
	}

	if pj.Name != "" {
		res.ProjectName = pj.Name
	}

	// Helper to check dep/devDep
	has := func(name string) bool {
		if pj.Deps != nil {
			if _, ok := pj.Deps[name]; ok {
				return true
			}
		}
		if pj.DevDeps != nil {
			if _, ok := pj.DevDeps[name]; ok {
				return true
			}
		}
		return false
	}

	// Detect frameworks (minimal v0.1)
	switch {
	case has("next"):
		res.Framework = "nextjs"
		if configMatches(dir, []string{"next.config.js", "next.config.mjs", "next.config.ts"}, `output\s*:\s*["']standalone["']`) {
			res.OutputDir = ".next/standalone"
			res.StartCommand = "node .next/standalone/server.js"
		} else {
			res.OutputDir = ".next"
			res.StartCommand = "npm run start"
			res.Warnings = append(res.Warnings, "Next.js detected without output: 'standalone'. Deploy will use .next plus installed production dependencies; standalone is still the leaner option.")
		}
	case has("@sveltejs/kit"):
		res.Framework = "sveltekit"
		if has("@sveltejs/adapter-node") || configUses(dir, []string{"svelte.config.js", "svelte.config.cjs", "svelte.config.mjs", "svelte.config.ts"}, "@sveltejs/adapter-node") {
			res.OutputDir = "build"
			res.StartCommand = "node build"
		} else {
			res.OutputDir = ""
			res.StartCommand = ""
			res.Warnings = append(res.Warnings, "SvelteKit detected without @sveltejs/adapter-node. Configure a deployable output and runtime.start manually.")
		}
	case has("@solidjs/start") || has("solid-start"):
		res.Framework = "solidstart"
		res.OutputDir = ".output"
		res.StartCommand = "node .output/server/index.mjs"
	case has("nuxt"):
		res.Framework = "nuxt"
		res.OutputDir = ".output"
		res.StartCommand = "node .output/server/index.mjs"
	case has("@astrojs/astro") || has("astro"):
		res.Framework = "astro"
		res.RuntimeType = "static"
		res.OutputDir = "dist"
		res.StartCommand = ""
	case has("vite") && !has("next") && !has("nuxt") && !has("@sveltejs/kit"):
		res.Framework = "vite-static"
		res.RuntimeType = "static"
		res.OutputDir = "dist"
		res.StartCommand = ""
	default:
		// remain auto
	}

	res.BuildCommand = scriptCommand(pj.Scripts, "build")
	if res.BuildCommand == "" {
		res.Warnings = append(res.Warnings, "No build script found in package.json. Set build.command manually.")
	}

	if res.Framework == "auto" {
		res.StartCommand = scriptCommand(pj.Scripts, "start")
		if res.StartCommand == "" {
			res.Warnings = append(res.Warnings, "No start script found in package.json. Set runtime.start manually.")
		}
	} else if res.RuntimeType == "static" && scriptCommand(pj.Scripts, "start") == "" {
		res.Warnings = append(res.Warnings, fmtStaticRuntimeWarning(res.OutputDir))
	}

	return res
}

func fmtStaticRuntimeWarning(outputDir string) string {
	if strings.TrimSpace(outputDir) == "" {
		return "Static framework detected. Consider setting runtime.type: static."
	}
	return "Static framework detected without a start script. Consider setting runtime.type: static and runtime.output: " + outputDir + "."
}

func scriptCommand(scripts map[string]string, name string) string {
	if scripts == nil {
		return ""
	}
	if _, ok := scripts[name]; !ok {
		return ""
	}
	return "npm run " + name
}

func configUses(dir string, names []string, needle string) bool {
	for _, name := range names {
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), needle) {
			return true
		}
	}
	return false
}

func configMatches(dir string, names []string, pattern string) bool {
	re := regexp.MustCompile(pattern)
	for _, name := range names {
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if re.Match(b) {
			return true
		}
	}
	return false
}
