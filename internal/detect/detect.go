package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Result struct {
	Framework    string
	ProjectName  string
	BuildCommand string
	StartCommand string
	OutputDir    string
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
		Framework:    "auto",
		ProjectName:  filepath.Base(dir),
		BuildCommand: "npm run build",
		StartCommand: "npm run start",
		OutputDir:    "dist",
	}

	p := filepath.Join(dir, "package.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return res
	}

	var pj pkgJSON
	if err := json.Unmarshal(b, &pj); err != nil {
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
		res.OutputDir = ".next"
		res.StartCommand = "npm run start"
	case has("@solidjs/start") || has("solid-start"):
		res.Framework = "solidstart"
		// Many SolidStart builds output to .output (Nitro-style) or dist depending on setup.
		// Keep it conservative:
		res.OutputDir = ".output"
		res.StartCommand = "node .output/server/index.mjs"
	case has("@sveltejs/kit"):
		res.Framework = "sveltekit"
		res.OutputDir = ".svelte-kit"
		res.StartCommand = "npm run preview"
	case has("nuxt"):
		res.Framework = "nuxt"
		res.OutputDir = ".output"
		res.StartCommand = "node .output/server/index.mjs"
	default:
		// remain auto
	}

	// Prefer real scripts if they exist
	if pj.Scripts != nil {
		if _, ok := pj.Scripts["build"]; ok {
			res.BuildCommand = "npm run build"
		}
		if _, ok := pj.Scripts["start"]; ok {
			res.StartCommand = "npm run start"
		}
		if _, ok := pj.Scripts["preview"]; ok && res.Framework == "sveltekit" {
			res.StartCommand = "npm run preview"
		}
	}

	return res
}
