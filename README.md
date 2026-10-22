# OraLab

**Run an Oracle Database 19c lab with one cross-platform command and manage its local resources from your browser.**

OraLab is a lightweight command-line wrapper around the Docker Engine API. It gives Windows, Linux, and macOS users the same commands for creating, starting, stopping, inspecting, and entering a local Oracle 19c container.

OraLab does not replace Oracle's tools. Inside the container, `sqlplus`, `rman`, `lsnrctl`, `expdp`, and `impdp` work with their normal Oracle syntax.

An optional interface at `http://127.0.0.1:8765` shows Docker and Oracle status and lets you adjust the container's CPU and memory limits. The UI is a local resource controller—not a database administration dashboard.

> [!IMPORTANT]
> OraLab manages a pinned official Oracle Database 19c Enterprise image internally: `container-registry.oracle.com/database/enterprise:19.3.0.0`. Users do not select or write an image name. Before the first pull, they must accept Oracle Container Registry terms and provide an Oracle Registry auth token when requested. See [Oracle registry access](#oracle-registry-access).

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
- An Oracle Container Registry account, accepted Oracle terms, and an auth token for the first image pull.
- Enough memory and disk space for Oracle Database and its persistent data.

The Docker CLI, Docker Compose, and Go are **not** runtime requirements. Go 1.24 or newer is needed only when building OraLab from source.

## Installation

OraLab publishes the same application version as a separate executable for Windows, Linux, and macOS. The installation wizard lets you select your operating system with the arrow keys, downloads the matching latest release, verifies its SHA-256 checksum, installs it for your user account, and adds it to `PATH` when necessary.

These builds currently target x86-64/AMD64 because OraLab's supported Oracle 19c image is published for `linux/amd64`.

### Installation wizard (recommended)

#### Step 1: Download the installer

Clone this repository:

```sh
git clone https://github.com/SABBAR-ZIYAD/OraLab.git
cd OraLab
```

If you already cloned it, open a terminal in the repository and update it instead:

```sh
git pull
```

#### Step 2: Run the installer

On Windows, use Command Prompt or PowerShell:

```bat
.\install.cmd
```

On Linux or macOS:

```sh
bash ./install.sh
```

#### Step 3: Select the operating system

Use <kbd>↑</kbd>/<kbd>↓</kbd> to select your operating system, then press <kbd>Enter</kbd>. The detected operating system is selected by default. The wizard will not install a binary for a different operating system or an unsupported CPU architecture.

The installer downloads the latest published OraLab release, verifies its SHA-256 checksum, installs it without administrator access, and adds its directory to your user `PATH` when necessary:

| Platform | Installation location |
| --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\OraLab\oralab.exe` |
| Linux | `~/.local/bin/oralab` |
| macOS | `~/.local/bin/oralab` |

#### Step 4: Open a new terminal

Close the current terminal and open a new one if the installer says it updated `PATH`. This makes the `oralab` command available everywhere.

#### Step 5: Verify the installation

```sh
oralab version
```

The command should print the installed version, for example `oralab 0.1.0`. Continue with [First-time setup](#first-time-setup) to configure and start Oracle.

For automated installation, skip the menu by passing the native operating system explicitly:

```powershell
.\install.cmd -OS windows
```

```sh
bash ./install.sh --os linux
bash ./install.sh --os darwin
```

The wizard installs the latest published GitHub release. It does not build the checked-out source and therefore does not require Go.

### Manual installation

Download the archive for your operating system from the [latest OraLab release](https://github.com/SABBAR-ZIYAD/OraLab/releases/latest):

- `oralab_VERSION_windows_amd64.zip`
- `oralab_VERSION_linux_amd64.tar.gz`
- `oralab_VERSION_darwin_amd64.tar.gz`

Extract the archive and move the executable into a directory on your `PATH`. The executable contains the configuration template it needs, so users do not need to keep the cloned repository after installation.

### Build from source

Contributors and users who prefer source builds need Go 1.24 or newer.

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

## First-time setup

All examples below use `oralab`. If it is not on your `PATH`, use the executable's full path instead.

### Step 1: Start Docker

Start Docker Desktop on Windows or macOS, or Docker Engine on Linux. Wait until Docker reports that the engine is running.

### Step 2: Create a lab directory

The lab directory stores the local configuration. It can be anywhere and does not need to be inside the cloned OraLab repository.

Windows Command Prompt:

```bat
mkdir "%USERPROFILE%\my-oracle-lab"
cd /d "%USERPROFILE%\my-oracle-lab"
```

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force "$HOME\my-oracle-lab"
Set-Location "$HOME\my-oracle-lab"
```

Linux or macOS:

```sh
mkdir -p ~/my-oracle-lab
cd ~/my-oracle-lab
```

### Step 3: Initialize the configuration

```sh
oralab init --cpus 4 --memory 6g --shm 1g
```

| Option | Explanation |
| --- | --- |
| `--cpus 4` | Limit the Oracle container to four CPU cores. The value must be a whole number. |
| `--memory 6g` | Limit the Oracle container to 6 GiB of memory. |
| `--shm 1g` | Give Oracle a 1 GiB shared-memory area. |
| `--password-stdin` | Optional. Read one password from standard input instead of prompting twice. Intended for automation. |

OraLab prompts twice for the database password without displaying it. It then creates `.env` and an embedded `.env.example` project marker. It refuses to overwrite an existing `.env`.

The saved `.env` contains the local database password and resource settings:

```dotenv
ORACLE_PWD=choose-a-local-lab-password
ORACLE_CPUS=4
ORACLE_MEMORY=6g
ORACLE_SHM_SIZE=1g
```

Do not commit or share `.env`. If you automate initialization, pass the password to `--password-stdin` without putting it directly in shell history.

### Step 4: Check the environment

```sh
oralab doctor
```

This checks the project configuration and confirms that OraLab can communicate with the local Docker Engine. Fix any reported problem before continuing.

### Step 5: Prepare Oracle Registry access

This step is required only when Docker needs to download the Oracle image.

1. Sign in at the [Oracle Container Registry](https://container-registry.oracle.com/).
2. Open the **database/enterprise** repository and accept its terms.
3. Create or obtain an Oracle Container Registry authentication token.
4. Set the username and token temporarily in the terminal that will run `oralab up`.

Windows Command Prompt:

```bat
set ORALAB_REGISTRY_USERNAME=your-oracle-account
set ORALAB_REGISTRY_TOKEN=your-auth-token
```

Windows PowerShell:

```powershell
$env:ORALAB_REGISTRY_USERNAME = 'your-oracle-account'
$env:ORALAB_REGISTRY_TOKEN = 'your-auth-token'
```

Linux or macOS:

```sh
export ORALAB_REGISTRY_USERNAME='your-oracle-account'
export ORALAB_REGISTRY_TOKEN='your-auth-token'
```

The variables must be supplied together. OraLab uses them for the image pull and does not save them in `.env`.

### Step 6: Download, create, and start Oracle

From the lab directory, run:

```sh
oralab up
```

OraLab will:

1. Connect to the local Docker Engine API.
2. Download its pinned Oracle Database 19c Enterprise image if it is not already available.
3. Create the persistent `oralab-oradata` volume when needed.
4. Create and start the managed `oralab-oracle` container.
5. Publish Oracle port `1521` on `127.0.0.1` only.
6. Wait until Oracle is ready.

The first image download and database initialization can take several minutes. The default startup timeout is 1,800 seconds (30 minutes). To allow 40 minutes instead:

```sh
oralab up --timeout 2400
```

After the image has been downloaded, normal `start` and `stop` operations do not need registry credentials. Clear the temporary values when the first startup finishes.

Windows Command Prompt:

```bat
set ORALAB_REGISTRY_USERNAME=
set ORALAB_REGISTRY_TOKEN=
```

Windows PowerShell:

```powershell
Remove-Item Env:ORALAB_REGISTRY_USERNAME
Remove-Item Env:ORALAB_REGISTRY_TOKEN
```

Linux or macOS:

```sh
unset ORALAB_REGISTRY_USERNAME ORALAB_REGISTRY_TOKEN
```

### Step 7: Verify the database

```sh
oralab status
oralab exec -- sqlplus / as sysdba
```

At the SQL prompt, run:

```sql
SELECT banner FROM v$version;
EXIT;
```

### Step 8: Create a regular database user (recommended)

`SYSDBA` has unrestricted administrative access. Create a separate user in the default pluggable database for exercises and application work:

```sh
oralab exec -- sqlplus / as sysdba
```

Then run the following SQL, replacing the example password:

```sql
ALTER SESSION SET CONTAINER = ORCLPDB1;

CREATE USER labuser IDENTIFIED BY "choose-a-different-password";
GRANT CREATE SESSION, CREATE TABLE, CREATE VIEW,
      CREATE SEQUENCE, CREATE PROCEDURE, CREATE TRIGGER
TO labuser;
ALTER USER labuser QUOTA UNLIMITED ON USERS;
EXIT;
```

Open a container shell and connect as the regular user. Omitting the password from the command makes SQL*Plus prompt for it:

```sh
oralab shell
sqlplus labuser@ORCLPDB1
```

Use `labuser` for everyday work, `SYSTEM` for ordinary administration, and `SYSDBA` only for database-level administration.

To connect as the built-in `SYSTEM` administrator instead, run `sqlplus system@ORCLPDB1` from `oralab shell` and enter the database password chosen during `oralab init`.

You can also connect from SQL Developer, DBeaver, or another client installed on the host:

| Setting | Value |
| --- | --- |
| Host | `127.0.0.1` |
| Port | `1521` |
| Connection type | Service name |
| Service name | `ORCLPDB1` |
| Username | `labuser` or `system` |
| Password | The password for the selected user |

## Everyday use

Run OraLab commands from the lab directory containing `.env`, or use `--project-dir` as described below.

### Start a work session

```sh
oralab status
oralab start
oralab shell
```

Inside the container, connect to the default pluggable database:

```sh
sqlplus labuser@ORCLPDB1
```

If `status` already reports that Oracle is running and ready, you do not need to run `start` again.

### Stop a work session

Exit SQL*Plus and the container shell, then stop Oracle gracefully:

```text
SQL> EXIT
$ exit
```

```sh
oralab stop
```

Stopping the container preserves the database in the `oralab-oradata` volume.

### Inspect status and logs

```sh
oralab status
oralab logs --no-follow --tail 200
```

To follow new log messages continuously, omit `--no-follow`. Press <kbd>Ctrl</kbd>+<kbd>C</kbd> to stop following the logs; this does not stop Oracle.

### Run Oracle tools

Open a Bash shell in the container:

```sh
oralab shell
```

Then use Oracle's tools normally:

```sh
sqlplus labuser@ORCLPDB1
sqlplus / as sysdba
rman target /
lsnrctl status
expdp help=y
impdp help=y
```

Or execute a tool directly from the host:

```sh
oralab exec -- sqlplus / as sysdba
oralab exec -- rman target /
oralab exec -- lsnrctl status
oralab exec -- expdp help=y
oralab exec -- impdp help=y
```

The `--` ends OraLab option processing. Everything after it is passed to the container as separate, unchanged arguments. OraLab returns the command's container exit status.

### Open the Resource Manager

```sh
oralab ui
```

This opens `http://127.0.0.1:8765`, where you can inspect status, monitor resource use, start or stop the container, and change its CPU and memory limits.

## Command reference

```text
oralab [--project-dir PATH] <command>
```

| Command | Description |
| --- | --- |
| `oralab doctor` | Check Docker Engine access and project configuration. |
| `oralab init --cpus N --memory S --shm S` | Securely collect a password and create `.env` with local resource limits. |
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

### Global option

| Option | Explanation |
| --- | --- |
| `--project-dir PATH` | Use the lab configuration at `PATH` instead of searching the current directory and its parents. Place this option before the command, for example `oralab --project-dir C:\labs\demo status`. |

### Initialization options

These options belong to `oralab init`:

| Option | Required | Explanation |
| --- | --- | --- |
| `--cpus COUNT` | Yes | Whole-number CPU limit for the Oracle container. |
| `--memory SIZE` | Yes | Container memory limit, such as `6g`. |
| `--shm SIZE` | Yes | Shared-memory size, such as `1g`. |
| `--password-stdin` | No | Read one password from standard input. Without it, OraLab securely prompts for the password twice. |

### Lifecycle options

| Command and option | Explanation |
| --- | --- |
| `oralab up --timeout SECONDS` | Create or start the container and wait up to this many seconds for Oracle readiness. The default is 1,800 seconds. |
| `oralab stop --timeout SECONDS` | Give Oracle up to this many seconds to stop gracefully. The default is 600 seconds. |
| `oralab restart --timeout SECONDS` | Use this shutdown timeout while gracefully restarting Oracle. The default is 600 seconds. |

Examples:

```sh
oralab up --timeout 2400
oralab stop --timeout 900
oralab restart --timeout 900
```

`oralab start` has no options. It starts an existing container and waits for Oracle readiness. Use `oralab up` for the first run because `up` can also download the image and create the container and volume.

### Log options

These options belong to `oralab logs` and can be combined:

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

Running `oralab logs` without options follows the log stream. Press <kbd>Ctrl</kbd>+<kbd>C</kbd> to return to the terminal.

### Resource Manager options

These options belong to `oralab ui`:

| Option | Explanation |
| --- | --- |
| `--port PORT` | Listen on a different loopback port instead of the default `8765`, for example `oralab ui --port 8877`. |
| `--no-open` | Start the Resource Manager without automatically opening a browser. |

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

It preserves the password, shared-memory setting, and unrelated variables. The UI cannot run SQL, open a terminal, alter Oracle SGA/PGA settings, change global Docker Desktop resources, or delete data.

## Configuration

| Variable | Required | Description |
| --- | --- | --- |
| `ORACLE_PWD` | Yes | Database password collected by `oralab init`. Treat it as a secret. |
| `ORACLE_CPUS` | Yes | Whole-number CPU limit selected during `oralab init` or managed from `oralab ui`. |
| `ORACLE_MEMORY` | Yes | Memory limit selected during `oralab init`, such as `6g`. |
| `ORACLE_SHM_SIZE` | Yes | Container shared-memory size selected during `oralab init`, such as `1g`. |

OraLab owns the image reference and does not read it from `.env`:

```text
container-registry.oracle.com/database/enterprise:19.3.0.0
```

OraLab currently creates the following Docker resources:

| Resource | Value |
| --- | --- |
| Container | `oralab-oracle` |
| Management label | `dev.oralab.managed=true` |
| Data volume | `oralab-oradata` |
| Container data path | `/opt/oracle/oradata` |
| Listener binding | `127.0.0.1:1521` → container port `1521` |

These details match the Oracle Database 19c Enterprise image managed by OraLab.

### Oracle registry access

Oracle distributes the default 19c image through the Oracle Container Registry. Sign in at [container-registry.oracle.com](https://container-registry.oracle.com/), open the **database/enterprise** repository, and accept the terms once for your Oracle account. Then set temporary credentials in the process environment before `oralab up`.

Linux/macOS:

```sh
export ORALAB_REGISTRY_USERNAME='your-account'
export ORALAB_REGISTRY_TOKEN='oracle-auth-token'
oralab up
```

Windows PowerShell:

```powershell
$env:ORALAB_REGISTRY_USERNAME = 'your-account'
$env:ORALAB_REGISTRY_TOKEN = 'oracle-auth-token'
oralab up
```

`ORALAB_REGISTRY_USERNAME` and `ORALAB_REGISTRY_TOKEN` must be supplied together. Use your Oracle account name and an Oracle Container Registry auth token; Oracle no longer accepts SSO passwords for registry CLI authentication. OraLab encodes the credentials into the Engine API pull request and does not write them to `.env`. Clear the variables when you are finished.

License acceptance is normally a one-time action for the Oracle account. Registry authentication is needed when downloading the image, not whenever the container starts. After the image is local, OraLab can start and stop the lab without contacting the registry. You must authenticate again on a new machine, after removing/resetting Docker's local images, or when OraLab needs to download another image version.

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

### `.env` already exists

This is intentional: `oralab init` will not overwrite passwords or local settings. Edit the existing file, or back it up and remove it yourself before deliberately initializing again.

### The image pull is unauthorized

Accept the database/enterprise terms in Oracle Container Registry and provide the temporary registry variables described under [Oracle registry access](#oracle-registry-access).

### The UI refuses a resource update

Resource changes require a local Docker endpoint and values within the CPU and memory capacity reported by Docker. Docker Desktop's virtual machine may expose less capacity than the physical host.

### Oracle does not become ready

Inspect the container output:

```sh
oralab logs --no-follow --tail 300 --timestamps
```

Review the logs for Oracle initialization or resource errors. OraLab's default image contract uses `ORACLE_PWD`, port `1521`, `/opt/oracle/oradata`, and the image's healthcheck.

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

## Publishing a release

Maintainers publish ready-to-run binaries by creating and pushing a semantic version tag from a tested commit:

```sh
git tag -a v0.1.0 -m "OraLab v0.1.0"
git push origin v0.1.0
```

The release workflow tests the source, builds Windows, Linux, and Intel macOS binaries, embeds `0.1.0` as the application version, creates compressed archives and SHA-256 checksums, and publishes a GitHub Release with generated notes. Pre-release tags such as `v0.2.0-rc.1` are published as GitHub pre-releases.

## Current project status

Implemented and covered by automated tests:

- Cross-platform command routing and project discovery.
- Safe `.env` setup.
- Direct Docker Engine API diagnostics and lifecycle operations.
- Image pull authentication, container logs, exec, and native exit statuses.
- Embedded localhost Resource Manager and live CPU/RAM updates.
- Persistent volume handling and non-destructive lifecycle commands.
- Automated release binaries for Windows, Linux, and Intel macOS.
- End-to-end Oracle 19c validation on Windows, including initialization, persistence, restart, SQL\*Plus, RMAN, listener status, and Data Pump availability.

Still required before OraLab can advertise a complete, ready-to-run Oracle 19c environment:

- Complete runtime testing on Linux and macOS hosts.
- Validate a full backup-and-recovery exercise beyond the current RMAN command check.
- Add platform code signing/notarization if OraLab is distributed beyond unsigned preview releases.

## Contributing

Issues and pull requests are welcome. Before submitting a change:

1. Keep the project focused on a lightweight local Oracle 19c lab.
2. Preserve native Oracle command behavior.
3. Do not add destructive database or volume operations.
4. Add or update tests for behavioral changes.
5. Run the commands in [Build and test](#build-and-test).

When reporting a problem, include your operating system, architecture, Docker version/endpoint, OraLab command, and sanitized error output. Never post `.env`, database passwords, or registry tokens.
