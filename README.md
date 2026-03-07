# eu-deploy

Licensed under Apache-2.0. See LICENSE and NOTICE.

## Current scope

`eu-deploy` is currently a small Go CLI for Node/npm web apps.

- `eu init` generates an `eudeploy.yaml` file with Node-oriented defaults.
- `eu deploy --target hetzner` prompts for missing server details, can seed env keys from `.env.example`, and saves the non-secret answers back to `eudeploy.yaml`.
- `eu build` runs the configured build command and packages the configured output folder.
- `eu deploy` supports local Docker and a single-VM Hetzner SSH deploy for `runtime.type: web`.

The checked-in sample config lives at `templates/node-web.yaml`. This repository itself is the CLI, not a deployable Node app.

Framework notes:
- Nuxt and SolidStart currently map to `.output` with a direct Node start command.
- SvelteKit is auto-configured only when `@sveltejs/adapter-node` is detected.
- Next.js with `output: 'standalone'` uses `.next/standalone` and a minimal runtime image.
- Next.js without standalone falls back to `.next` plus installed production dependencies.
- For Next.js standalone builds, `eu build` stages `public/` and `.next/static/` into `.next/standalone/` before packaging so `server.js` has the assets it expects.
- For standard Next.js builds, `eu build` stages `.next/` and `public/` together so `next start` can run from the packaged artifact.

## Build artifact

```bash
go build -o eu ./cmd/eu
./eu build
```

This creates `.eudeploy/<project-name>.tar.gz` and `.eudeploy/build.json`.
Deploy will reuse the artifact only when the current build inputs still match the stored metadata.

Optional live smoke test for the standalone Next.js path:

```bash
EUDEPLOY_E2E=1 go test ./internal/build -run TestNextStandaloneE2E -count=1
```

Optional live Docker smoke test for the standalone Next.js path:

```bash
EUDEPLOY_DOCKER_E2E=1 go test ./internal/deploy -run TestNextStandaloneDockerE2E -count=1
```

## Hetzner deploy

Prerequisites:
- A Linux VM reachable over SSH
- Docker installed on the VM
- DNS for your hostname pointed at the VM before you expect TLS to succeed

Run:

```bash
./eu init
./eu deploy --target hetzner
```

On the first deploy, the CLI will prompt for:
- public hostname
- Hetzner server host/IP
- SSH user, port, and optional key path
- remote app path and loopback service port
- deploy env values for keys found in `env.*` or seeded from `.env.example`

The Hetzner target uploads the build artifact, generates a remote Docker image, runs the app on `127.0.0.1:<service_port>`, and starts Caddy in Docker to terminate TLS and reverse proxy the hostname.

## Local deploy (Docker)

Prerequisite: Docker installed and running.

```bash
./eu init
# edit eudeploy.yaml (e.g., SolidStart: build.output=.output, runtime.start=node .output/server/index.mjs)
./eu deploy
```

This builds (if needed), creates a local Docker image, and runs it at `http://localhost:<port>`.

Flags:
- `--target docker` (default)
- `--port <hostPort>` override host port (default: runtime.port)
- `--detach` run container in background
- `--name <containerName>` override container name (default: `eu-<project>`)
