# eu-deploy

Licensed under Apache-2.0. See LICENSE and NOTICE.

## Build artifact

```bash
go build -o eu ./cmd/eu
./eu build
```

This creates `.eudeploy/<project-name>.tar.gz` and `.eudeploy/build.json`.

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
