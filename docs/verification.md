# Verification

## Automated checks

Run from the repository root:

```console
go test ./...
go vet ./...
```

The unit suite uses a fake subprocess runner. It does not download Oracle or require registry credentials. It covers Compose command construction, native argument forwarding, TTY selection, safe init/stop behavior, project resolution, configuration embedding, and Manager Host/Origin/CSRF/no-exec boundaries.

Validate the manifest with a test-only password supplied through the process environment:

```powershell
$env:ORACLE_PWD = "SmokeOnlyPassword1"
docker compose -f compose.yaml config --quiet
Remove-Item Env:ORACLE_PWD
```

The release workflow builds Windows AMD64, Linux AMD64, and macOS AMD64 executables. Public CI does not pull the gated Oracle Enterprise image.

## Credentialed Oracle acceptance test

On an authorized AMD64 test machine with accepted Oracle registry terms:

1. Create a fresh lab and run `oralab init`, `docker login container-registry.oracle.com`, and `oralab up`.
2. Confirm `oralab status` reports `running`, and `healthy`/`ready` when the image exposes health.
3. Run `oralab sqlplus / as sysdba`, `oralab rman target /`, and `oralab lsnrctl status`.
4. Check `oralab exec dbca -help`, plus `expdp` and `impdp` help or a disposable export/import.
5. Create a PDB test user and table, insert a marker row, run `oralab stop` and `oralab up`, and query the marker again.
6. Load `http://127.0.0.1:8765`, refresh bounded logs, and exercise confirmed lifecycle controls. Verify the page survives closure of the original `oralab up` terminal and becomes unavailable after Oracle stops.
7. Test `https://127.0.0.1:5500/em/` separately and record whether database-side configuration is needed. Do not mark EM Express verified based only on port mapping.
8. Confirm listener/EM/Manager are not bound to `0.0.0.0`.
9. Stop the lab. Confirm `docker volume inspect oralab-oradata` still succeeds.

## Current verification boundary

Repository tests and Compose configuration validation can run without Oracle credentials. A real image pull, full first initialization, persistence cycle, native utility inventory, EM Express login, architecture behavior, and detached Manager lifecycle require the credentialed environment above. Do not infer those results from mocks.

Development verification on 2026-10-23 covered `go test ./...`, `go vet ./...`, Windows/Linux/macOS AMD64 cross-builds, Docker Compose v5.3.1 manifest rendering on Windows, PowerShell and shell installer syntax, and a localhost Manager HTTP/security-header/state smoke test. The execution sandbox could not connect to the user's Docker named pipe, so it did not start, stop, migrate, or modify the existing Oracle container/volume. The gated Oracle-image, persistence, native-utility, detached-Manager, and EM Express acceptance checks remain deliberately unclaimed until run in the credentialed host environment.
