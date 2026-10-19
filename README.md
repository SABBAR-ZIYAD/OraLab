# OraLab

**A lightweight, cross-platform command line for running an Oracle Database 19c lab with Docker.**

OraLab gives Windows, Linux, and macOS users the same small set of commands for setting up, starting, stopping, inspecting, and entering a local Oracle 19c container. Oracle's own tools are not renamed or reimplemented: inside the container you still use `sqlplus`, `rman`, `lsnrctl`, `expdp`, and `impdp` exactly as you would in an Oracle installation.

An optional browser interface at `127.0.0.1` shows local Docker and Oracle status and lets you control the lab's CPU and memory limits. It is a resource companion, not a web-based database administration tool.

> [!IMPORTANT]
> The OraLab CLI and local Resource Manager are implemented. The repository does **not yet include the Oracle `compose.yaml` or a selected and validated Oracle 19c image**, so database lifecycle commands currently stop with a clear “environment has not been implemented yet” message. See [Project status](#project-status) and [PROJECT_PLAN.md](PROJECT_PLAN.md).

## What this repository provides

- One `oralab` command with the same interface on Windows, Linux, and macOS.
- Safe project setup from `.env.example` without overwriting an existing `.env`.
- Docker and project health checks with `oralab doctor`.
- Oracle container lifecycle commands built on Docker Compose.
- An interactive container shell and unchanged native Oracle command passthrough.
- A dependency-free, localhost-only CPU/RAM Resource Manager embedded in the executable.
- Standalone release builds for Windows AMD64, Linux AMD64/ARM64, and macOS Intel/Apple Silicon.
- No reset, destroy, prune, or volume-deletion command.

OraLab deliberately does not provide a SQL editor, web terminal, DBA dashboard, user portal, remote API, or replacement Oracle utilities.

## Requirements

End users need:

- A supported 64-bit Windows, Linux, or macOS system.
- Docker Desktop or Docker Engine with Docker Compose v2.
- Enough local memory and disk space for the Oracle image and database files.
- Access to the Oracle 19c image selected by the project once that phase is complete, including any required registry sign-in or license acceptance.

Go is required only when building OraLab from source.

## Install OraLab

Download the executable for your operating system and architecture from the repository's GitHub Releases page:

| System | Release file | Installed name |
| --- | --- | --- |
| Windows x64 | `oralab-windows-amd64.exe` | `oralab.exe` |
| Linux x64 | `oralab-linux-amd64` | `oralab` |
| Linux ARM64 | `oralab-linux-arm64` | `oralab` |
| macOS Intel | `oralab-darwin-amd64` | `oralab` |
| macOS Apple Silicon | `oralab-darwin-arm64` | `oralab` |

Verify the downloaded file against `SHA256SUMS.txt` from the same release before installing it.

### Windows

Rename the downloaded file to `oralab.exe`, place it in a directory on `PATH`, then check it from PowerShell:

```powershell
oralab version
```

### Linux and macOS

Rename the downloaded file, make it executable, and move it to a directory on `PATH`:

```sh
chmod +x oralab
sudo mv oralab /usr/local/bin/oralab
oralab version
```

If macOS blocks a downloaded executable, review it in **System Settings → Privacy & Security** before allowing it. Do not bypass operating-system security checks for a file you have not verified.

## Quick start

Run these commands from the repository directory:

```sh
# Check Docker and the project files
oralab doctor

# Create .env from .env.example (never overwrites an existing .env)
oralab setup

# Review and complete the local settings before starting Oracle
# Windows: notepad .env
# Linux/macOS: use your preferred text editor

# Start Oracle and wait for its Compose healthcheck
oralab up

# Inspect the service
oralab status

# Enter the Oracle container
oralab shell
```

Once inside the container, use native Oracle tools unchanged:

```sh
sqlplus / as sysdba
rman target /
lsnrctl status
expdp help=y
impdp help=y
```

The startup wait defaults to 1,800 seconds because the first Oracle database initialization can take time. Select a different positive timeout when needed:

```sh
oralab up --timeout 2400
```

> [!NOTE]
> The quick-start database steps describe the intended completed workflow. They require the pending `compose.yaml` and validated Oracle image.

## Command reference

General syntax:

```text
oralab [--project-dir PATH] <command>
```

| Command | Purpose |
| --- | --- |
| `oralab doctor` | Check the Docker CLI, Compose v2, project files, `.env`, and Compose configuration. |
| `oralab setup` | Create `.env` from `.env.example`; refuse to overwrite an existing file. |
| `oralab ui` | Start the local Resource Manager and open it in the default browser. |
| `oralab up [--timeout SECONDS]` | Create/start Oracle and wait for its Compose healthcheck; default timeout is 1,800 seconds. |
| `oralab status` | Show the Oracle Compose service status. |
| `oralab logs [COMPOSE LOG OPTIONS]` | Follow Oracle logs and pass supported log options to Docker Compose. |
| `oralab shell` | Open an interactive Bash shell in the Oracle container. |
| `oralab exec -- COMMAND [ARGS...]` | Run a native command in the Oracle container with its arguments unchanged. |
| `oralab start` | Start an existing stopped Oracle container. |
| `oralab stop [--timeout SECONDS]` | Gracefully stop Oracle; default shutdown timeout is 600 seconds. |
| `oralab restart [--timeout SECONDS]` | Gracefully restart Oracle; default shutdown timeout is 600 seconds. |
| `oralab version` | Print the OraLab version. |
| `oralab help` | Print command-line help. |

### Run from outside the repository

OraLab normally searches the current directory and its parents for an OraLab project. To operate on a project elsewhere, provide its directory explicitly:

```sh
oralab --project-dir /path/to/oracle19c status
```

On Windows:

```powershell
oralab --project-dir C:\labs\oracle19c status
```

You can also set `ORALAB_PROJECT_DIR` as the default project location:

```sh
export ORALAB_PROJECT_DIR=/path/to/oracle19c
oralab status
```

```powershell
$env:ORALAB_PROJECT_DIR = 'C:\labs\oracle19c'
oralab status
```

An explicit `--project-dir` takes priority over `ORALAB_PROJECT_DIR`.

## Use native Oracle commands

OraLab handles the local Docker environment; it does not change the Oracle command-line experience.

### Interactive shell

```sh
oralab shell
```

Then run Oracle commands normally:

```sh
sqlplus / as sysdba
rman target /
lsnrctl status
```

### Direct command passthrough

Use `oralab exec --` to invoke an Oracle tool without first opening a shell. The `--` ends OraLab option processing; all remaining arguments are sent separately and unchanged to the container.

```sh
oralab exec -- sqlplus / as sysdba
oralab exec -- rman target /
oralab exec -- lsnrctl status
oralab exec -- expdp help=y
oralab exec -- impdp help=y
```

OraLab returns the exit status from Docker Compose or the native command to the calling terminal.

## Local Resource Manager

Start the embedded interface with:

```sh
oralab ui
```

OraLab listens on `http://127.0.0.1:8765` and opens the default browser. It requires no separate web server, Node.js installation, account, or cloud service.

Available options:

```sh
# Start without opening a browser
oralab ui --no-open

# Use another loopback port
oralab ui --port 8877
```

The page displays:

- Host operating system and architecture.
- Docker availability, context, engine capacity, and endpoint type.
- Oracle container state and database readiness.
- Configured and effective CPU/RAM limits.
- Live CPU and memory usage.
- Persistent container mounts.

The page can:

- Save whole-number CPU and GiB memory limits.
- Start, stop, or restart the Oracle service.
- Recreate a running container only after explicit restart confirmation.
- Verify Docker's effective limits after applying a change.

The Resource Manager changes only these values in `.env`:

```dotenv
ORACLE_CPUS=4
ORACLE_MEMORY=6g
```

Other settings—including `ORACLE_IMAGE`, `ORACLE_PWD`, and unrelated variables—are preserved. The future `compose.yaml` must consume `ORACLE_CPUS` and `ORACLE_MEMORY` for the limits to take effect.

The UI cannot run SQL or shell commands, modify SGA/PGA settings, change global Docker Desktop or WSL limits, alter credentials, delete volumes, or listen for remote users. It binds to IPv4 loopback only and rejects mutation requests from unrelated browser origins or remote Docker contexts.

## Configuration

`oralab setup` copies `.env.example` to a local `.env` with restrictive permissions where the operating system supports them. It will not replace an existing `.env`.

Current settings are:

| Variable | Meaning |
| --- | --- |
| `ORACLE_IMAGE` | Exact Oracle 19c container image to run. Pending project validation. |
| `ORACLE_PWD` | Local database password expected by the selected image. Treat it as a secret. |
| `ORACLE_CPUS` | Whole-number CPU limit managed by the Resource Manager. |
| `ORACLE_MEMORY` | Memory limit in GiB, stored with a `g` suffix and managed by the Resource Manager. |

`.env` is excluded from Git. Never commit passwords, registry tokens, or other local secrets.

## Common workflows

Follow logs until you press <kbd>Ctrl</kbd>+<kbd>C</kbd>:

```sh
oralab logs
```

Pass Docker Compose log options when useful:

```sh
oralab logs --tail 200
oralab logs --since 10m
```

Stop Oracle gracefully and keep its container/data:

```sh
oralab stop
```

Start that existing container again:

```sh
oralab start
```

Restart with a custom shutdown allowance:

```sh
oralab restart --timeout 900
```

OraLab intentionally has no destructive cleanup command. Any manual Docker volume removal is outside OraLab and can permanently delete the database.

## Troubleshooting

### “No OraLab project found”

Run the command inside this repository, in one of its subdirectories, or specify its location:

```sh
oralab --project-dir /path/to/oracle19c doctor
```

### “compose.yaml does not exist”

This is expected in the current implementation phase. The CLI and UI are present, but the validated Oracle environment is not yet committed.

### Docker is unavailable

Start Docker Desktop or the Docker Engine, confirm `docker --version` and `docker compose version` work in the same terminal, then run:

```sh
oralab doctor
```

### `.env` already exists

OraLab refuses to overwrite it to protect passwords and local settings. Edit the existing file, or back it up and remove it yourself before deliberately running `oralab setup` again.

### The UI refuses resource changes

Resource mutations require a local Docker context and values within the CPU/memory capacity reported by Docker. If Docker Desktop's virtual machine has less capacity than the physical host, OraLab uses the Docker-reported capacity.

## Build and test from source

Install Go 1.22 or newer, then run:

```sh
go test ./...
go vet ./...
go build -o oralab ./cmd/oralab
```

The browser code can also be syntax-checked with Node.js when available:

```sh
node --check cmd/oralab/web/app.js
```

The GitHub Actions workflow runs the tests and static checks, cross-compiles all supported release targets, and publishes checksums for version tags matching `v*`.

## Project status

Implemented and tested:

- Cross-platform OraLab command routing.
- Safe `.env` creation.
- Docker/Compose diagnostics.
- Native command passthrough behavior.
- Embedded localhost Resource Manager and its request protections.
- Resource-file preservation and Docker state parsing.
- Cross-platform release builds.

Still required before the repository is a complete Oracle 19c lab:

- Select a redistributable or officially obtainable Oracle 19c image and document its license/access requirements.
- Add the single-service `compose.yaml`.
- Define and verify the Oracle healthcheck, localhost listener binding, and persistent volumes.
- Validate initialization, restart, persistence, SQL*Plus, RMAN, listener, Data Pump, and recovery behavior against the exact image.
- Complete real runtime testing on every supported host platform.

The detailed implementation and validation contract is in [PROJECT_PLAN.md](PROJECT_PLAN.md). The compact CLI behavior reference is in [docs/oralab.md](docs/oralab.md).

## Safety and scope

OraLab is intended for a local lab, not production database hosting. Its design keeps the boundary narrow:

- The Oracle listener is planned to bind to localhost by default.
- The browser UI binds only to `127.0.0.1`.
- Docker Compose configuration is validated without printing interpolated secrets.
- Running resource changes require explicit restart confirmation.
- No endpoint exposes SQL, a terminal, arbitrary host commands, or volume deletion.
- Oracle administration remains in Oracle's native terminal tools.

See [PROJECT_PLAN.md](PROJECT_PLAN.md) for the full scope and acceptance criteria.
