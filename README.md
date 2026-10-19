# OraLab

**Run an Oracle Database 19c lab with one cross-platform command and manage its local resources from your browser.**

OraLab is a lightweight command-line wrapper around the Docker Engine API. It gives Windows, Linux, and macOS users the same commands for creating, starting, stopping, inspecting, and entering a local Oracle 19c container.

OraLab does not replace Oracle's tools. Inside the container, `sqlplus`, `rman`, `lsnrctl`, `expdp`, and `impdp` work with their normal Oracle syntax.

An optional interface at `http://127.0.0.1:8765` shows Docker and Oracle status and lets you adjust the container's CPU and memory limits. The UI is a local resource controller—not a database administration dashboard.

> [!IMPORTANT]
> OraLab's CLI, direct Docker Engine integration, and browser UI are implemented. The project has not yet selected and validated one exact Oracle 19c image. You must provide `ORACLE_IMAGE` yourself, and verify that the image's license, environment variables, healthcheck, and storage paths match OraLab's current container contract. See [Current project status](#current-project-status).

## Why OraLab?

- **One command everywhere:** use the same `oralab` interface on Windows, Linux, and macOS.
- **Native Oracle experience:** Oracle commands and arguments pass through unchanged.
- **No Compose dependency:** OraLab talks directly to Docker Engine using Go.
- **Local resource controls:** inspect and update CPU/RAM limits from a localhost-only UI.
- **Persistent database storage:** database files live in the named `oralab-oradata` volume.
- **Safe defaults:** the Oracle listener and web UI bind to loopback, and OraLab has no reset, prune, destroy, or volume-delete command.
- **Single executable:** the browser application is embedded in the OraLab binary.

## Requirements

To run OraLab, you need:

- A 64-bit Windows, Linux, or macOS computer.
- Docker Desktop or Docker Engine running locally.
- Go 1.24 or newer while tagged release binaries are not yet available.
- Access to a compatible Oracle Database 19c container image.
- Enough memory and disk space for Oracle Database and its persistent data.

The Docker CLI and Docker Compose are **not** runtime requirements.

## Installation

There is no tagged release yet, so build the current version from source.

### Windows PowerShell

```powershell
git clone https://github.com/SABBAR-ZIYAD/OraLab.git
Set-Location OraLab
go build -o oralab.exe ./cmd/oralab
./oralab.exe version
```

You can keep `oralab.exe` in the repository or move it to a directory included in your `PATH`.

### Linux and macOS

```sh
git clone https://github.com/SABBAR-ZIYAD/OraLab.git
cd OraLab
go build -o oralab ./cmd/oralab
./oralab version
```

To make it available system-wide:

```sh
sudo install -m 0755 oralab /usr/local/bin/oralab
oralab version
```

## Quick start

All examples below use `oralab`. If you kept the newly built binary in the repository, use `./oralab` on Linux/macOS or `./oralab.exe` in PowerShell instead.

### 1. Check the environment

From the cloned OraLab repository, run:

```sh
oralab doctor
```

This checks the Docker Engine connection and reports any missing project configuration.

### 2. Create the local configuration

```sh
oralab setup
```

This copies `.env.example` to `.env`. OraLab refuses to overwrite an existing `.env`.

Open `.env` and set at least the image and password expected by your chosen Oracle image:

```dotenv
ORACLE_IMAGE=your-compatible-oracle-19c-image:tag
ORACLE_PWD=choose-a-local-lab-password

ORACLE_CPUS=4
ORACLE_MEMORY=6g
ORACLE_SHM_SIZE=1g
```

Never commit `.env`; it is excluded from Git.

### 3. Create and start Oracle

```sh
oralab up
```

OraLab will:

1. Connect to the local Docker Engine API.
2. Pull `ORACLE_IMAGE` if it is not already available locally.
3. Create the `oralab-oradata` named volume when needed.
4. Create the managed `oralab-oracle` container when needed.
5. Publish Oracle port `1521` on `127.0.0.1` only.
6. Start the container and wait for its image healthcheck. If the image has no healthcheck, OraLab treats a running container as ready.

The first Oracle initialization can take several minutes. The default startup timeout is 1,800 seconds:

```sh
oralab up --timeout 2400
```

### 4. Check status and logs

```sh
oralab status
oralab logs --tail 200
```

Run `oralab logs` without `--no-follow` to stream logs until you press <kbd>Ctrl</kbd>+<kbd>C</kbd>.

### 5. Use Oracle's native tools

Open a Bash shell in the container:

```sh
oralab shell
```

Then use Oracle normally:

```sh
sqlplus / as sysdba
rman target /
lsnrctl status
expdp help=y
impdp help=y
```

Or execute a native tool directly from the host:

```sh
oralab exec -- sqlplus / as sysdba
oralab exec -- rman target /
oralab exec -- lsnrctl status
oralab exec -- expdp help=y
oralab exec -- impdp help=y
```

The `--` ends OraLab option processing. Everything after it is passed to the container as separate, unchanged arguments. OraLab returns the command's container exit status.

## Command reference

```text
oralab [--project-dir PATH] <command>
```

| Command | Description |
| --- | --- |
| `oralab doctor` | Check Docker Engine access and project configuration. |
| `oralab setup` | Safely create `.env` from `.env.example`. |
| `oralab up [--timeout SECONDS]` | Create/start Oracle and wait for readiness. Default: 1,800 seconds. |
| `oralab status` | Show Docker Engine and Oracle container status. |
| `oralab logs [OPTIONS]` | Read or follow Oracle container logs. |
| `oralab shell` | Open an interactive Bash shell in the Oracle container. |
| `oralab exec -- COMMAND [ARGS...]` | Run a command inside the container without changing its arguments. |
| `oralab start` | Start the existing Oracle container. |
| `oralab stop [--timeout SECONDS]` | Gracefully stop Oracle. Default: 600 seconds. |
| `oralab restart [--timeout SECONDS]` | Gracefully restart Oracle. Default: 600 seconds. |
| `oralab ui [--port PORT] [--no-open]` | Start the localhost Resource Manager. |
| `oralab version` | Print the OraLab version. |
| `oralab help` | Print command-line help. |

### Log options

| Option | Meaning |
| --- | --- |
| `--no-follow` | Print available logs and exit instead of following the stream. |
| `--tail LINES` | Return only the requested number of recent lines. |
| `--since VALUE` | Return logs after a Docker-compatible timestamp or relative duration such as `10m`. |
| `--timestamps` | Include a timestamp on each log line. |

Examples:

```sh
oralab logs --no-follow --tail 100
oralab logs --since 10m --timestamps
```

## Local Resource Manager

Start the embedded web interface with:

```sh
oralab ui
```

OraLab opens `http://127.0.0.1:8765` in your default browser. No Node.js server, account, or cloud service is required.

Useful options:

```sh
# Do not open the browser automatically
oralab ui --no-open

# Select another loopback port
oralab ui --port 8877
```

The dashboard displays:

- Host operating system and architecture.
- Docker availability, version, endpoint, CPUs, and memory.
- Oracle container state and readiness.
- Configured and effective Oracle CPU/RAM limits.
- Live Oracle container CPU and memory use.
- Persistent mounts attached to the container.

From the UI you can:

- Start, stop, and restart the Oracle container.
- Save whole-number CPU and GiB memory limits.
- Apply new limits live through Docker's container-update API.
- Verify the limits Docker actually applied.

The resource editor changes only these `.env` values:

```dotenv
ORACLE_CPUS=4
ORACLE_MEMORY=6g
```

It preserves the image, password, shared-memory setting, and unrelated variables. The UI cannot run SQL, open a terminal, alter Oracle SGA/PGA settings, change global Docker Desktop resources, or delete data.

## Configuration

| Variable | Required | Description |
| --- | --- | --- |
| `ORACLE_IMAGE` | Yes | Exact Oracle 19c image reference used to create the container. |
| `ORACLE_PWD` | Image-dependent | Database password passed to the selected image. Treat it as a secret. |
| `ORACLE_CPUS` | No | Whole-number CPU limit. Can be managed from `oralab ui`. |
| `ORACLE_MEMORY` | No | Memory limit in GiB with a `g` suffix, such as `6g`. Can be managed from the UI. |
| `ORACLE_SHM_SIZE` | No | Container shared-memory size. Defaults to `1g` when empty. |

OraLab currently creates the following Docker resources:

| Resource | Value |
| --- | --- |
| Container | `oralab-oracle` |
| Management label | `dev.oralab.managed=true` |
| Data volume | `oralab-oradata` |
| Container data path | `/opt/oracle/oradata` |
| Listener binding | `127.0.0.1:1521` → container port `1521` |

These details must be compatible with the Oracle image you select.

### Private registry authentication

When the image registry requires authentication, set temporary credentials in the process environment before `oralab up`.

Linux/macOS:

```sh
export ORALAB_REGISTRY_USERNAME='your-account'
export ORALAB_REGISTRY_TOKEN='temporary-token-or-password'
export ORALAB_REGISTRY_SERVER='container-registry.example.com'
oralab up
```

Windows PowerShell:

```powershell
$env:ORALAB_REGISTRY_USERNAME = 'your-account'
$env:ORALAB_REGISTRY_TOKEN = 'temporary-token-or-password'
$env:ORALAB_REGISTRY_SERVER = 'container-registry.example.com'
oralab up
```

`ORALAB_REGISTRY_USERNAME` and `ORALAB_REGISTRY_TOKEN` must be supplied together. OraLab encodes the credentials into the Engine API pull request and does not write them to `.env`. Clear the variables when you are finished.

## Use OraLab from another directory

OraLab searches the current directory and its parents for the project. To target it explicitly:

```sh
oralab --project-dir /path/to/OraLab status
```

```powershell
oralab --project-dir C:\labs\OraLab status
```

You can also define a default location:

```sh
export ORALAB_PROJECT_DIR=/path/to/OraLab
oralab status
```

```powershell
$env:ORALAB_PROJECT_DIR = 'C:\labs\OraLab'
oralab status
```

An explicit `--project-dir` takes precedence over `ORALAB_PROJECT_DIR`.

## Common operations

```sh
# Show current state
oralab status

# Follow startup/runtime logs
oralab logs

# Stop gracefully while keeping the container and database volume
oralab stop

# Start the same container again
oralab start

# Restart with a longer shutdown timeout
oralab restart --timeout 900
```

OraLab intentionally does not include a destructive cleanup command. Manually deleting `oralab-oradata` can permanently destroy the lab database.

## Troubleshooting

### No OraLab project found

Run the command from the cloned repository or one of its subdirectories. Otherwise, pass the project path explicitly:

```sh
oralab --project-dir /path/to/OraLab doctor
```

### Docker Engine is unavailable

Start Docker Desktop or Docker Engine, then check the connection:

```sh
oralab doctor
```

OraLab reads the standard Docker environment configuration, including the local endpoint selected through `DOCKER_HOST`.

### `ORACLE_IMAGE` is empty

Run `oralab setup`, edit `.env`, and provide a compatible image reference. OraLab deliberately has no unvalidated default Oracle image.

### `.env` already exists

This is intentional: `oralab setup` will not overwrite passwords or local settings. Edit the existing file, or back it up and remove it yourself before running setup again.

### The image pull is unauthorized

Accept any terms required by the registry, confirm the image reference, and provide the temporary registry variables described under [Private registry authentication](#private-registry-authentication).

### The UI refuses a resource update

Resource changes require a local Docker endpoint and values within the CPU and memory capacity reported by Docker. Docker Desktop's virtual machine may expose less capacity than the physical host.

### Oracle does not become ready

Inspect the container output:

```sh
oralab logs --no-follow --tail 300 --timestamps
```

Confirm that your selected image supports the configured environment variables, exposes port `1521`, stores data at `/opt/oracle/oradata`, and defines a meaningful healthcheck.

## Security and scope

OraLab is designed for a **local development and learning lab**, not production database hosting.

- The browser server binds only to IPv4 loopback (`127.0.0.1`).
- The Oracle listener is published only on `127.0.0.1:1521`.
- State-changing operations and container command execution reject remote Docker endpoints.
- Browser mutations are protected against unrelated hosts and origins.
- The web application exposes no SQL editor, shell, arbitrary host command, or volume-delete endpoint.
- Registry credentials remain process variables and are never stored by OraLab.
- `.env` is ignored by Git and should never be committed.

## Build and test

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o oralab ./cmd/oralab
```

When Node.js is installed, validate the embedded browser JavaScript with:

```sh
node --check cmd/oralab/web/app.js
```

## Current project status

Implemented and covered by automated tests:

- Cross-platform command routing and project discovery.
- Safe `.env` setup.
- Direct Docker Engine API diagnostics and lifecycle operations.
- Image pull authentication, container logs, exec, and native exit statuses.
- Embedded localhost Resource Manager and live CPU/RAM updates.
- Persistent volume handling and non-destructive lifecycle commands.
- Source builds for Windows, Linux, and macOS.

Still required before OraLab can advertise a complete, ready-to-run Oracle 19c environment:

- Select and document one legally obtainable Oracle 19c image.
- Validate the container environment, port, data path, and inherited healthcheck against that exact image.
- Test initialization, persistence, restart, SQL\*Plus, RMAN, listener, Data Pump, and recovery workflows.
- Complete runtime testing on each supported host platform.

## Contributing

Issues and pull requests are welcome. Before submitting a change:

1. Keep the project focused on a lightweight local Oracle 19c lab.
2. Preserve native Oracle command behavior.
3. Do not add destructive database or volume operations.
4. Add or update tests for behavioral changes.
5. Run the commands in [Build and test](#build-and-test).

When reporting a problem, include your operating system, architecture, Docker version/endpoint, OraLab command, and sanitized error output. Never post `.env`, database passwords, or registry tokens.
