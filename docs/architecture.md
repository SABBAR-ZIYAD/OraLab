# Architecture

OraLab V2 uses one lifecycle authority: Docker Compose.

```text
terminal / browser
       |
       v
OraLab Go executable ---- fixed argument arrays ----> docker compose
       |                                               |
       | localhost Manager                             v
       +---- http://127.0.0.1:8765          service: oracle
                                              |  official image, unchanged
                                              |  127.0.0.1:1521
                                              |  127.0.0.1:5500
                                              v
                              external volume: oralab-oradata
                              mount: /opt/oracle/oradata
```

## Compose and Oracle

The checked-in and embedded `compose.yaml` defines the stable service name `oracle` and project name `oralab`. The default is the unmodified official image `container-registry.oracle.com/database/enterprise:19.3.0.0`. OraLab does not replace its entrypoint, command, binaries, initialization flow, or health check, and it never builds or distributes Oracle software.

The listener and EM Express container ports are published only on IPv4 loopback. The data volume is external and has the fixed Docker name `oralab-oradata`, mounted at `/opt/oracle/oradata`. Compose stopping or recreating the service therefore does not remove its files. OraLab exposes no public cleanup command and never invokes `down -v`.

`.env` supplies the database initialization values and resource limits. `compose.yaml` supplies the image. Routine commands never render or rewrite either file. The CLI invokes `docker` directly with argument arrays and an explicit project directory, environment file, and Compose file, so paths containing spaces do not require shell quoting inside OraLab.

## Custom image safety

Advanced users can edit `services.oracle.image` directly. OraLab asks Compose for the rendered value and stores the last successfully started image in `.oralab-state`. A later mismatch is blocked before `up`; it is treated as a possible database-format change, not an upgrade. The state file is a guardrail, not an Oracle compatibility oracle. Backups and migrations remain the user's responsibility.

All utility aliases continue to address `services.oracle` and run as the `oracle` OS user. A compatible replacement image must therefore support the same environment, data layout, user, and utilities. Image-dependent behavior is diagnosed rather than patched.

## Manager security boundary

The Manager is the same small Go executable running as a detached host-side process. It binds only to `127.0.0.1`, calls the same fixed Compose adapter, and exits after the Oracle service stops. No Docker socket is mounted into a web container.

The HTTP layer validates the Host header, restricts Origin for mutations, issues an HTTP-only SameSite cookie, requires a matching per-process CSRF capability header, sets a restrictive Content Security Policy, and exposes only state, bounded logs, and the fixed start/stop/restart actions. There is no raw Docker, shell, SQL, utility, mount, or volume-deletion endpoint. A local process running as the same OS user can still control the browser or Docker; OraLab is a single-user development tool, not a remote multi-user control plane.

EM Express is separate Oracle software on HTTPS port 5500. Mapping its port does not prove that the database-side feature is configured or available, and the Manager does not imitate it.

## Design decision

V1's custom Moby/Docker Engine orchestration and live resource editor were removed. Compose now owns creation, lifecycle, configuration rendering, logging, and exec. The retained Go code covers user-friendly commands, conservative migration/image guards, readiness interpretation, native-tool forwarding, and the localhost UI. This reduces dependencies and leaves `compose.yaml` auditable and user-configurable.

See [ADR 0001](adr-0001-compose-lifecycle.md) for the recorded decision and consequences.
