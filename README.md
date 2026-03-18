# eu-deploy

Licensed under Apache-2.0. See LICENSE and NOTICE.

## Current scope

`eu-deploy` is currently a small Go CLI for Node/npm web apps.

- `eu init` generates an `eudeploy.yaml` file with Node-oriented defaults.
- `eu deploy --target <provider>` prompts for missing server details, can seed env keys from `.env.example`, and saves the non-secret answers back to `eudeploy.yaml`.
- `eu preflight` checks SSH, Docker, DNS, remote paths, ports, and TLS prerequisites before you deploy.
- `eu bootstrap` prepares a fresh SSH-reachable VM with Docker and the directory layout that `eu-deploy` expects.
- `eu analytics install` uploads the analytics worker to a remote VM, initializes analytics tables in the shared PostgreSQL container, installs cron jobs for log processing and daily aggregation, and creates both MaxMind env and `GeoIP.conf` templates for weekly GeoLite refreshes.
- `eu build` runs the configured build command and packages the configured output folder.
- `eu deploy` supports local Docker plus single-VM SSH deploys for `runtime.type: web` on Hetzner, Scaleway, and OVH, including shared PostgreSQL, release history, rollback, and an optional post-deploy command inside the app container.

The checked-in sample config lives at `templates/node-web.yaml`. This repository itself is the CLI, not a deployable Node app.

## Commands

`eu init`
- Detects the current app and writes `eudeploy.yaml`.
- Run this once at the start of a project, then review the generated config.

`eu build`
- Runs the configured build command and packages the output into `.eudeploy/`.
- Run this when you want to verify the artifact locally before deployment.

`eu bootstrap`
- Connects to the configured remote VM over SSH and prepares it for `eu-deploy`.
- Use this first on a fresh server. It can install Docker, create the expected directories, optionally configure UFW and fail2ban, and start a shared PostgreSQL container when `database.mode: shared` is configured.

`eu analytics install`
- Connects to the configured remote VM over SSH and installs the analytics worker plus wrapper scripts under `<server_path>/analytics` and `<server_path>/scripts`.
- Initializes `analytics_projects`, `analytics_buffer`, `analytics_daily`, and `analytics_processor_state` in the shared PostgreSQL container on that VM.
- Installs cron jobs for:
  - processing Caddy JSON access logs every 5 minutes
  - aggregating the previous UTC day at `00:05`
  - refreshing GeoLite2 City and ASN once per week
- Creates `<server_path>/analytics/maxmind/maxmind.env` and `<server_path>/analytics/maxmind/GeoIP.conf`; fill in either config before expecting GeoLite enrichment. The refresh wrapper prefers `geoipupdate` when installed and otherwise falls back to the direct download flow.

`eu preflight`
- Checks whether the configured remote VM looks ready for deployment.
- Use this before the first deploy and whenever DNS, SSH, ports, or Docker may have changed.

`eu deploy`
- `eu deploy` uses local Docker by default.
- `eu deploy --target hetzner|scaleway|ovh` builds the app if needed, uploads the artifact, starts or updates the app container, reloads the shared Caddy proxy config on the server, can provision one PostgreSQL database/user per app on the same VM, can run a configured post-deploy command for migrations/setup, and keeps the last three release images for rollback.
- Add `--json` to emit a single machine-readable result object instead of human-oriented text. In JSON mode, the command does not prompt for missing config values.
- The JSON payload includes an ordered `phases` array with stable IDs so a UI can map deploy progress and failures consistently.

`eu logs`
- Streams remote Docker logs for the active app container, the shared proxy, or the shared PostgreSQL container.
- Add `--json` to emit newline-delimited JSON log events (`stdout` / `stderr`) instead of plain text.

`eu releases`
- Lists remote release history, newest first.
- Add `--json` to emit a machine-readable release list with stable field names and a `current` marker.

`eu destroy`
- Removes the remote app deployment, deletes its site proxy snippet, and can optionally drop the app database and role when shared PostgreSQL is enabled.
- Add `--json` for a machine-readable result object.

`eu rollback`
- Re-activates the previous distinct remote release, or a specific release ID that is still present in remote history.
- Rollback does not revert database schema changes automatically.
- Add `--json` for a machine-readable result object.

## Typical usage

Local Docker:

```bash
./eu init
./eu build
./eu deploy
```

Machine-readable examples:

```bash
./eu build --json
./eu preflight --target hetzner --json
./eu deploy --target hetzner --json
./eu logs --target hetzner --component app --json
./eu releases --target hetzner --json
./eu rollback --target hetzner --json
./eu destroy --target hetzner --json
```

- `build`, `bootstrap`, `preflight`, `deploy`, `releases`, `destroy`, and `rollback` emit one JSON object.
- `logs --json` emits one JSON object per log line so a UI can stream it.
- `deploy --json` includes ordered `phases` entries with `id`, `label`, and `status`.

First deploy to a new remote VM:

```bash
./eu init
# review eudeploy.yaml and set the real hostname
./eu bootstrap
./eu preflight
./eu deploy --target hetzner
```

Repeat deploy to an existing VM:

```bash
./eu preflight
./eu deploy --target hetzner
```

Adding another website to the same Hetzner server:

```bash
./eu init
# set a different hostname, app_path, and service_port
./eu preflight
./eu deploy --target hetzner
```

Using one shared PostgreSQL service on the same server:

```yaml
database:
  mode: shared
  shared:
    engine: postgres
    version: "16"
    name: my_app
    user: my_app
```

- `eu bootstrap` starts one shared `eu-shared-postgres` container on `127.0.0.1:5432`.
- `eu deploy --target hetzner|scaleway|ovh` creates or updates the app-specific PostgreSQL role and database and injects `DATABASE_URL` into the app container automatically.
- You can make `eu-deploy` run migrations or setup scripts automatically by adding a `deploy.post_deploy` command and any extra files that command needs:

```yaml
deploy:
  post_deploy:
    command: node scripts/setup-db.js
    include:
      - scripts/setup-db.js
```

- The listed `include` paths are copied into the runtime image before the post-deploy command runs.
- For local setup or migrations, tunnel into the server:

```bash
ssh -L 5432:127.0.0.1:5432 root@your-server
```

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

## Remote deploy

Prerequisites:
- A Linux VM reachable over SSH
- DNS for your hostname pointed at the VM before you expect TLS to succeed

Run:

```bash
./eu init
./eu bootstrap
./eu preflight
./eu deploy --target hetzner
```

On the first Hetzner workflow, the CLI will prompt for:
- public hostname
- provider server host/IP
- SSH user, port, and optional key path
- remote server root, app path, and loopback service port
- deploy env values for keys found in `env.*` or seeded from `.env.example`

`eu bootstrap` installs Docker on Debian/Ubuntu-style hosts, enables the Docker daemon, optionally configures UFW and fail2ban, and creates the expected `server_path`, app, and shared proxy directories.

When `database.mode: shared` is enabled, `eu bootstrap` also creates the shared Docker network and starts a shared PostgreSQL container bound to `127.0.0.1:5432` on the server.

`eu preflight` verifies:
- local `ssh` and `scp`
- the remote server address resolves
- your hostname resolves and whether it matches the server IP
- SSH connectivity
- remote Docker CLI and daemon access
- write access to the configured server and app paths
- whether ports `80/443` are free or already owned by the shared proxy
- whether the app's loopback `service_port` is free
- when `database.mode: shared` is enabled, whether the shared Docker network exists and whether PostgreSQL is already running or port `5432` is blocked by something else

The remote targets upload the build artifact, generate a remote Docker image, run the app on `127.0.0.1:<service_port>` and `127.0.0.1:<service_port+1>`, and reload a shared Caddy container on the server. That shared proxy model is what allows multiple small websites to coexist on one VM:

- one Caddy container owns ports `80/443`
- each app deploy uses two loopback ports for blue/green style cutover
- each deploy writes one per-site Caddy snippet keyed by hostname
- each deploy also writes a project-scoped JSON access log block to `/var/log/caddy/<project>.access.log`
- each deploy keeps the last three release images and a small release history for rollback
- optional: one shared PostgreSQL container serves multiple apps, each with its own database and role
- optional: a post-deploy hook can run inside the app container before deploy healthcheck succeeds

## Forgejo Actions

For a Vercel-style flow, use:

- IDE -> Forgejo
- Forgejo Actions runner -> `eu deploy --target <provider> --no-prompt`
- remote VM -> internet

The runner can inject non-interactive deploy values from environment variables such as:

- `EUDEPLOY_HETZNER_HOST`
- `EUDEPLOY_HETZNER_USER`
- `EUDEPLOY_HETZNER_PORT`
- `EUDEPLOY_HETZNER_SSH_KEY_PATH`
- `EUDEPLOY_HETZNER_SERVER_PATH`
- `EUDEPLOY_HETZNER_APP_PATH`
- `EUDEPLOY_HETZNER_SERVICE_PORT`
- `EUDEPLOY_ROUTE_HOSTNAME`
- `EUDEPLOY_ENV_JWT_SECRET`
- `EUDEPLOY_ENV_ADMIN_EMAIL`
- `EUDEPLOY_ENV_ADMIN_PASSWORD`

If your Forgejo runner ever leaves queued jobs unassigned after a failed workflow, install the included watchdog templates:

- `scripts/forgejo_runner_watchdog.py`
- `templates/forgejo-runner-watchdog.service`
- `templates/forgejo-runner-watchdog.timer`

The watchdog checks for stale queued jobs with no assigned task and restarts the runner automatically.

Operational commands:

```bash
./eu logs --target hetzner --component app
./eu releases --target hetzner
./eu destroy --target hetzner
./eu destroy --target hetzner --drop-database
./eu rollback --target hetzner
./eu rollback --target hetzner --to 20260308-101530-deadbeef1234
```

The SSH provider logic is shared across:
- `--target hetzner`
- `--target scaleway`
- `--target ovh`

Example multi-site layout:

```text
massage.example.com -> 127.0.0.1:3001
yoga.example.com    -> 127.0.0.1:3002
pilates.example.com -> 127.0.0.1:3003
```

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
