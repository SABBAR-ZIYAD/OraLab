# ADR 0001: Docker Compose owns OraLab lifecycle

- Status: accepted
- Date: 2026-10-23

## Context

OraLab V1 used the Moby Go client to create and update a fixed Oracle container. That duplicated lifecycle behavior already provided by Docker Compose, hardcoded image selection in runtime code, increased the dependency and test surface, and made a user-auditable image change impossible. Giving a browser service an unrestricted Docker socket would create an unacceptable host-control boundary.

## Decision

Docker Compose V2 is the only Oracle lifecycle authority. The service name is `oracle`, the project name is `oralab`, and the external data volume remains `oralab-oradata`. The image reference comes from the non-overwritten `compose.yaml`. The Go CLI invokes `docker compose` with argument arrays, supplies an explicit project directory/file/env file, and adds conservative legacy-container and image/data guards.

The Manager remains a detached localhost host process using the same narrow Compose adapter. It has fixed state/log/lifecycle endpoints and no Docker socket or generic command endpoint. The old Moby orchestration and live container-update code is removed.

## Consequences

Users do not need Compose knowledge for normal commands, while technical users can audit or replace the image reference. OraLab depends on the Docker CLI plus Compose V2 and starts several short-lived subprocesses for state. Advanced image compatibility cannot be proven automatically, so mismatches are blocked and documented as explicit Oracle backup/migration work rather than upgrades.
