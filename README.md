# OraLab

OraLab is a small, local development wrapper around Docker Compose and Oracle Database. It runs the unmodified official Oracle Database 19c Enterprise image, keeps database files in the external Docker volume `oralab-oradata`, publishes Oracle only on loopback, and exposes the image's native tools through short commands. Docker Compose is the lifecycle source of truth; OraLab never rebuilds Oracle and never deletes database data.

## Requirements

- Docker Desktop or Docker Engine with Docker Compose V2 (`docker compose version`)
- An Intel/AMD 64-bit environment supported by the Oracle image
- At least 6 GiB of memory available to Docker
- At least 20 GiB free disk space; 30 GiB is recommended for the image, database files, and growth
- An Oracle Container Registry account and acceptance of the Database Enterprise image terms

OraLab's CLI is published for Windows AMD64, Linux AMD64, and macOS Intel. CLI availability does not imply that Oracle's image supports every host architecture. ARM hosts may need emulation and are best-effort only.

## Install

From the repository on Windows:

```powershell
.\install.cmd
```

On Linux or macOS:

```sh
./install.sh
```

Open a new terminal and check the installation:

```console
oralab version
```

## First run

Step 1 — create and enter a dedicated lab directory:

```console
mkdir my-oracle-lab
cd my-oracle-lab
```

Step 2 — create the local configuration. The defaults are 4 CPUs, 6 GiB RAM, and 1 GiB shared memory:

```console
oralab init
```

You can override those defaults:

```console
oralab init --cpus 4 --memory 6g --shm 1g
```

`init` creates `.env`, `.env.example`, and `compose.yaml` without overwriting existing files. The password in `.env` is local and must not be committed.

Step 3 — once, sign in at [Oracle Container Registry](https://container-registry.oracle.com/), accept the Database Enterprise terms, then let Docker store the registry credential using its normal login flow:

```console
docker login container-registry.oracle.com
```

Step 4 — start the lab. The first image download and database initialization can take a while:

```console
oralab up
```

Step 5 — verify and connect:

```console
oralab status
oralab shell
```

Inside SQL*Plus, switch to the default pluggable database when creating normal lab users:

```sql
ALTER SESSION SET CONTAINER = ORCLPDB1;
```

## Everyday use

```console
oralab up                         Start the lab and wait for Oracle readiness
oralab status                     Show the selected image and Oracle state
oralab shell                      Open SQL*Plus as local OS-authenticated SYSDBA
oralab stop                       Gracefully stop Oracle and preserve all data
oralab logs                       Show the latest 200 Oracle log lines
oralab logs --follow              Follow Oracle logs until Ctrl+C
oralab doctor                     Diagnose the local configuration
oralab ui                         Open the localhost manager
```

Native Oracle utilities are passed directly to the running Compose service named `oracle`:

```console
oralab sqlplus / as sysdba
oralab rman target /
oralab lsnrctl status
oralab expdp ...
oralab impdp ...
oralab exec dbca -help
```

OraLab preserves each argument and the native command's exit status. It does not interpret SQL, mount arbitrary host files, or start a second Oracle container for a utility command.

## Connections and interfaces

- Oracle listener: `127.0.0.1:1521`
- Default SID: `ORCLCDB`
- Default PDB/service: `ORCLPDB1`
- OraLab Manager: `http://127.0.0.1:8765`
- Oracle EM Express candidate URL: `https://127.0.0.1:5500/em/`

Port 5500 is mapped locally, but EM Express availability depends on the selected Oracle image and database-side configuration. OraLab reports it as a feature to verify rather than changing the database automatically. A local HTTPS certificate warning is normal when EM Express uses its self-signed certificate.

## Advanced image customization

The default image is:

```text
container-registry.oracle.com/database/enterprise:19.3.0.0
```

Technical users may change only the `image:` value under the `oracle` service in `compose.yaml`. That file is the source of truth and OraLab does not rewrite it during `up`, `stop`, `status`, `doctor`, utility execution, or UI use. All commands continue to target the Compose service named `oracle`.

Image substitution is best-effort and is not an automatic database upgrade. You are responsible for selecting an official image compatible with the environment variables, tools, health behavior, architecture, and existing database files. After a successful new-lab start—or during a guarded V1 handover—OraLab records the selected image in `.oralab-state`; changing it while reusing an existing data volume is refused. Back up and migrate the database using Oracle-supported procedures or use a separately prepared empty volume—OraLab never upgrades, overwrites, or deletes database files.

## Safety boundaries

- `oralab stop` uses Compose stop; it does not run `down -v`.
- The external volume `oralab-oradata` is never deleted by OraLab.
- Listener, EM Express, and Manager ports bind to `127.0.0.1` only.
- OraLab refuses automatic takeover when it detects the legacy `oralab-oracle` container.
- The Manager has no generic shell or SQL endpoint and never receives an unrestricted Docker socket.
- Oracle Database is proprietary software governed by Oracle's separate license and registry terms. OraLab does not redistribute Oracle software.

## More documentation

- [Command reference](docs/commands.md)
- [Architecture and security](docs/architecture.md)
- [Compose lifecycle decision](docs/adr-0001-compose-lifecycle.md)
- [Safe migration from V1](docs/migration.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Verification](docs/verification.md)
