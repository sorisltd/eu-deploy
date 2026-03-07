package detect

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		wantFramework    string
		wantBuildCommand string
		wantOutputDir    string
		wantStartCommand string
		warningSubstring string
	}{
		{
			name:             "generic-node",
			wantFramework:    "auto",
			wantBuildCommand: "npm run build",
			wantOutputDir:    "dist",
			wantStartCommand: "npm run start",
		},
		{
			name:             "nuxt",
			wantFramework:    "nuxt",
			wantBuildCommand: "npm run build",
			wantOutputDir:    ".output",
			wantStartCommand: "node .output/server/index.mjs",
		},
		{
			name:             "solidstart",
			wantFramework:    "solidstart",
			wantBuildCommand: "npm run build",
			wantOutputDir:    ".output",
			wantStartCommand: "node .output/server/index.mjs",
		},
		{
			name:             "sveltekit-node",
			wantFramework:    "sveltekit",
			wantBuildCommand: "npm run build",
			wantOutputDir:    "build",
			wantStartCommand: "node build",
		},
		{
			name:             "sveltekit-no-adapter",
			wantFramework:    "sveltekit",
			wantBuildCommand: "npm run build",
			wantOutputDir:    "",
			wantStartCommand: "",
			warningSubstring: "adapter-node",
		},
		{
			name:             "nextjs-standalone",
			wantFramework:    "nextjs",
			wantBuildCommand: "npm run build",
			wantOutputDir:    ".next/standalone",
			wantStartCommand: "node .next/standalone/server.js",
		},
		{
			name:             "nextjs-no-standalone",
			wantFramework:    "nextjs",
			wantBuildCommand: "npm run build",
			wantOutputDir:    ".next",
			wantStartCommand: "npm run start",
			warningSubstring: "leaner option",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Detect(filepath.Join("testdata", tc.name))

			if got.Framework != tc.wantFramework {
				t.Fatalf("framework mismatch: got %q want %q", got.Framework, tc.wantFramework)
			}
			if got.BuildCommand != tc.wantBuildCommand {
				t.Fatalf("build command mismatch: got %q want %q", got.BuildCommand, tc.wantBuildCommand)
			}
			if got.OutputDir != tc.wantOutputDir {
				t.Fatalf("output dir mismatch: got %q want %q", got.OutputDir, tc.wantOutputDir)
			}
			if got.StartCommand != tc.wantStartCommand {
				t.Fatalf("start command mismatch: got %q want %q", got.StartCommand, tc.wantStartCommand)
			}

			if tc.warningSubstring == "" {
				return
			}

			for _, warning := range got.Warnings {
				if strings.Contains(warning, tc.warningSubstring) {
					return
				}
			}

			t.Fatalf("expected warning containing %q, got %v", tc.warningSubstring, got.Warnings)
		})
	}
}
