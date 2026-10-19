# OraLab command reference

OraLab provides the same project lifecycle commands on Windows, Linux and
macOS. It does not replace, rename or imitate Oracle Database commands.

## Project commands

```text
oralab doctor
oralab setup
oralab ui
oralab up
oralab status
oralab logs
oralab shell
oralab start
oralab stop
oralab restart
oralab exec -- <native-command> [arguments...]
```

Run a command from anywhere by supplying the repository location:

```text
oralab --project-dir /path/to/oracle19c up
```

On Windows, the path can use normal Windows syntax:

```powershell
oralab --project-dir C:\labs\oracle19c up
```

`ORALAB_PROJECT_DIR` can provide the default location when commands are run
outside the repository.

## Local CPU/RAM resource manager

```text
oralab ui
```

The command starts a dependency-free browser interface on
`http://127.0.0.1:8765` and opens it automatically. Optional flags are:

```text
oralab ui --no-open
oralab ui --port 8877
```

The interface reads the active Docker context, Docker Engine CPU/memory
capacity, Compose service, container health, effective limits, statistics and
mounts. Resource edits update only these `.env` keys:

```text
ORACLE_CPUS=4
ORACLE_MEMORY=6g
```

The selected `compose.yaml` must use those variables for the Oracle service's
CPU and memory limits. OraLab verifies the values reported by `docker inspect`
after application and reports a mismatch rather than claiming success.

Applying changes to a running Oracle container requires explicit restart
confirmation. No UI action deletes volumes, changes credentials, modifies
Oracle SGA/PGA parameters, controls global Docker Desktop/WSL resources, or
executes SQL and shell commands.

## Native Oracle commands

Enter the container and use Oracle's native interface unchanged:

```text
oralab shell
```

```sh
sqlplus / as sysdba
rman target /
lsnrctl status
expdp help=y
impdp help=y
```

Alternatively, pass a native command directly into the container. The `--`
ends OraLab option processing; everything after it is passed to the Oracle
container as separate, unchanged arguments.

```text
oralab exec -- sqlplus / as sysdba
oralab exec -- rman target /
oralab exec -- lsnrctl status
oralab exec -- expdp help=y
oralab exec -- impdp help=y
```

The exit status from Docker Compose or the native command is returned to the
calling terminal.

## Safety behavior

- `oralab setup` creates `.env` with restrictive permissions where supported
  and refuses to overwrite an existing file.
- `oralab doctor` validates Compose with `docker compose config --quiet`, so
  interpolated configuration and passwords are not printed.
- `oralab up` waits for the Compose healthcheck rather than sleeping for an
  arbitrary period.
- `oralab ui` listens on IPv4 loopback only, rejects non-local Docker contexts
  for mutations, validates host/origin headers and protects changes with a
  session-bound request token.
- Stops and restarts allow Oracle up to 600 seconds to shut down by default.
- OraLab deliberately has no reset, destroy, prune or volume-deletion command.

OraLab requires a completed `compose.yaml` with a service named `oracle`. Until
that environment is implemented and verified, lifecycle commands fail clearly
instead of substituting an unverified image or configuration.

## Building

Contributors with Go 1.22 or newer can run:

```text
go test ./...
go build ./cmd/oralab
```

Tagged releases build standalone executables for Windows AMD64, Linux AMD64,
Linux ARM64, Intel macOS and Apple Silicon macOS. End users do not need Go.
