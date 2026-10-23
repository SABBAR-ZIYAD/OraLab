# Troubleshooting

## Registry login or pull failure

Sign in at `https://container-registry.oracle.com/`, open Database > enterprise, accept its terms, and then run:

```console
docker login container-registry.oracle.com
oralab up
```

Docker—not OraLab—owns registry credential storage. Inspect Docker Desktop's credential-store configuration if your security policy requires it.

## First start takes a long time

The image is large and first database creation is much slower than later starts. Use `oralab logs --follow` in another terminal. `oralab up --timeout 3600` permits an hour. A running container without a healthy status is still initializing; do not interrupt it merely because SQL connections are not ready yet.

## Docker or Compose is unavailable

Run `docker version` and `docker compose version`. Start Docker Desktop/Engine and ensure the active context is local. OraLab rejects remote Docker endpoints because its ports, paths, browser UI, and data-safety assumptions are local-machine assumptions.

## Architecture, memory, or storage

The default Oracle 19c Enterprise image is intended for AMD64. OraLab's CLI may compile on other systems even when the Oracle image cannot run natively. On ARM hosts, emulation is best-effort and needs substantially more time/resources.

Allocate at least 6 GiB RAM to Docker. Keep at least 20 GiB free, with 30 GiB recommended for normal growth. Docker Desktop stores images and named volumes inside its VM/WSL disk, so host free space and the Docker disk limit both matter.

## Port collision

Ports 1521, 5500, and 8765 are deliberately fixed to loopback. Find and stop the local program already using the reported port; OraLab does not silently publish Oracle on a public or unexpected alternative. On Windows, `netstat -ano | findstr :1521` can identify the PID.

## EM Express is unavailable

`https://127.0.0.1:5500/em/` is only a candidate URL. Confirm Oracle is ready, inspect `oralab logs`, and check the selected image's documented EM Express support and database-side HTTPS configuration. A self-signed certificate warning is expected locally. OraLab does not alter XDB/EM configuration automatically and does not claim availability merely because the port is mapped.

## SQL login and containers

`oralab shell` uses local OS authentication inside the container and opens the CDB root as SYSDBA. Use `ALTER SESSION SET CONTAINER = ORCLPDB1;` for PDB administration. Normal users should connect to the PDB service rather than the CDB root.

If a utility says the service is not running, run `oralab status`, then `oralab up`. OraLab intentionally does not start or initialize a database implicitly from `exec`.

## Host files and Data Pump

Host and container paths are different. OraLab does not mount arbitrary working directories. Use an explicitly reviewed `docker cp` workflow for transient SQL files, or add a deliberate bind mount to `compose.yaml` after considering permissions and portability. Data Pump `DIRECTORY` values are Oracle directory objects, not host filesystem paths.

## Legacy container or existing volume warning

Stop. Do not rename, remove, or attach anything until you read [migration.md](migration.md), inspect the mounts, and verify a database backup. The refusal prevents two Oracle instances from sharing one data volume.

## Image-change refusal

OraLab found that `compose.yaml` selects a different image from `.oralab-state`. Restore the prior image, or perform an explicit Oracle-supported migration. Image substitution is never treated as an in-place upgrade.
