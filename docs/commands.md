# Command reference

Run commands from the lab directory, or prefix any command with `--project-dir PATH`. OraLab searches parent directories for `.env.example`, so commands also work in subdirectories of a lab.

## Core workflow

### `oralab init [--cpus N] [--memory SIZE] [--shm SIZE] [--password-stdin]`

For a new lab, creates `.env`, `.env.example`, `compose.yaml`, and—only when it does not already exist—the external `oralab-oradata` volume. Defaults are 4 CPUs, 6 GiB RAM, and 1 GiB shared memory. It refuses to overwrite either user-owned file, refuses an unknown existing data volume, and refuses a detected V1 container.

For an explicit V1 handover after the stopped legacy container has been manually removed, plain `oralab init` can install only the missing V2 files. It requires the existing volume to carry OraLab's management label, preserves `.env` byte-for-byte, does not create or replace the volume, and records the original image guard. See [migration.md](migration.md) before using this path.

Interactive use prompts twice for the database password. For automation, send exactly one password on standard input:

```powershell
$env:ORALAB_PASSWORD | oralab init --password-stdin
```

Do not put a real password in shell history. OraLab writes `.env` with mode `0600` where the platform supports Unix permissions; protect access to the lab directory on Windows as well.

### `oralab up [--timeout SECONDS]`

Validates Docker Compose, reads the Oracle image from `compose.yaml`, rejects an unsafe legacy-container or image/data combination, runs `docker compose up --detach oracle`, and waits for Compose-visible readiness. The default timeout is 1800 seconds because first initialization can be slow. It also starts the localhost Manager when possible.

`up` is idempotent. It does not force recreation, delete the container, or delete the volume.

### `oralab status`

Shows the Compose project, selected image, container state, health/readiness interpretation, listener, PDB service, and the conditional EM Express URL. This command does not modify the lab.

### `oralab stop [--timeout SECONDS]`

Gracefully stops the `oracle` service with a default 600-second timeout. It never calls `docker compose down`, never passes `-v`, and never removes `oralab-oradata`.

### `oralab doctor`

Checks Docker Compose V2, local context policy, required files, rendered `services.oracle.image`, legacy-container conflicts, and image-safety state. Diagnostics do not print `ORACLE_PWD`.

## Native Oracle utilities

All utility commands use `docker compose exec --user oracle oracle ...`. Arguments are passed as separate process arguments without a host shell. OraLab preserves standard input, output, error, Ctrl+C behavior through the Docker CLI, and the utility exit status. A TTY is requested only when both input and output are real terminals; pipes use Compose `--no-tty`.

```console
oralab shell
oralab sqlplus / as sysdba
oralab rman target /
oralab lsnrctl status
oralab expdp labuser/... DIRECTORY=DATA_PUMP_DIR DUMPFILE=lab.dmp
oralab impdp labuser/... DIRECTORY=DATA_PUMP_DIR DUMPFILE=lab.dmp
oralab exec dbca -help
```

`shell` is exactly the convenient alias for `sqlplus / as sysdba`; it is not a Bash shell. `exec` runs an installed container utility and never creates a second container. Utilities can differ in a user-selected image; unsupported programs fail with their normal Compose/container error.

Piping example:

```powershell
Get-Content .\check.sql | oralab sqlplus -s labuser@ORCLPDB1
```

Host paths are not automatically container paths. SQL scripts and Data Pump files require an explicit copy or mount workflow designed by the user; OraLab does not silently add host mounts.

## Logs and UI

### `oralab logs [--follow] [--tail LINES] [--since VALUE] [--timestamps]`

Without options, prints the latest 200 Oracle service lines and exits. `--follow` streams until interrupted. `--tail`, `--since`, and `--timestamps` map to Docker Compose log options. The deprecated V1 `--no-follow` flag is accepted but is now the default behavior.

### `oralab ui [--port PORT] [--no-open]`

Starts the Manager on IPv4 loopback, default port 8765, and opens a browser unless `--no-open` is supplied. The Manager provides status, fixed lifecycle actions, connection details, configuration display, and a bounded 200-line log view. It provides no arbitrary exec or SQL endpoint.

## Informational and compatibility commands

- `oralab version` prints the executable version.
- `oralab help` prints compact built-in help.
- `oralab setup` remains as a deprecated alias for `init`.
- `oralab start` remains as a deprecated alias for `up`.
- `oralab restart` remains as a deprecated safe `stop` followed by `up` sequence.

OraLab intentionally has no reset, destroy, prune, volume-delete, or automatic-upgrade command.
