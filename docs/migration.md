# Safe migration from OraLab V1

V1 created a container named `oralab-oracle` directly through the Docker Engine API. V2 uses the Compose service `oracle`; both versions refer to the external volume `oralab-oradata`. They must never run at the same time against that volume.

| V1 | V2 |
|---|---|
| Direct Engine API lifecycle | `docker compose` lifecycle |
| Container `oralab-oracle` | Compose-owned `oralab-oracle-1` (name may vary by Compose) |
| Image fixed in Go runtime | Image sourced from `compose.yaml` |
| `.env` plus embedded runtime settings | `.env`, non-overwritten `compose.yaml`, `.oralab-state` guard |
| Live resource editor | Read-only Compose limits; edit `.env`, then stop/up |
| Logs followed by default | Last 200 lines by default; `--follow` is explicit |

## Before handover

1. Keep the currently installed V1 binary available until the backup is verified.
2. With V1 still installed, run `oralab status`, then `oralab stop`. Confirm the database is stopped.
3. Inspect ownership without changing it:

   ```console
   docker inspect oralab-oracle
   docker volume inspect oralab-oradata
   ```

   Confirm the legacy container mounts `oralab-oradata` at `/opt/oracle/oradata`.
4. Make and test an Oracle-supported logical or physical backup suitable for your database. Do not treat a copy of live database files as a valid backup. Data Pump or RMAN should be run and verified according to Oracle documentation and your recovery requirements.
5. Save a protected copy of the existing `.env`. Never post it in an issue or commit it.

## Explicit handover

V2 deliberately refuses to proceed while the exact legacy container `oralab-oracle` exists. After the old database is stopped, the backup is verified, and the volume mapping above is confirmed, remove only the stopped legacy container metadata:

```console
docker rm oralab-oracle
```

This command does not remove a named volume unless a volume-removal option is added. Do not add `-v`, do not run `docker volume rm`, and do not run a system prune.

Install V2, return to the existing lab directory, and run plain `oralab init`. After confirming the legacy container is absent and `oralab-oradata` has OraLab's ownership label, V2 installs its embedded `.env.example` and `compose.yaml`, records the original image guard, and preserves the existing `.env` byte-for-byte. Do not pass resource/password options during this handover. Then run:

```console
oralab init
oralab doctor
oralab up
oralab status
oralab shell
```

On the first successful V2 start, `.oralab-state` records the image read from `compose.yaml`. V2 reuses the external named volume and does not initialize a separate project-prefixed volume.

## Rollback

Stop V2 first with `oralab stop`. Do not let V1 and V2 containers coexist against the volume. If rollback is required, inspect the Compose container and volume mapping, remove only the stopped V2 service container with an explicit Docker Compose command, then recreate the legacy container using the original V1 version and configuration. Never delete `oralab-oradata`. Restore the pre-migration backup if Oracle reports a data-format problem.

Because container handover and database compatibility cannot be proven from metadata alone, OraLab does not automate these container removals or claim the handover is a database upgrade.

## Changing the Oracle image

Changing `image:` is supported for technical users on a best-effort basis, but reusing existing database files with a different image may be unsafe. OraLab blocks an image mismatch recorded in `.oralab-state`. Restore the original image to keep using the volume, or perform an Oracle-supported backup/migration into a separately prepared compatible database. Never delete the state file merely to bypass the warning.
