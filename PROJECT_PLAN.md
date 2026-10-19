# Oracle Database 19c DBA Docker Lab — V1 Execution Plan

> **For the implementation agent.** Build a minimal, genuine Oracle Database 19c **command-line DBA environment** that is easy for students and teachers to install, start, use, stop, and resume across Windows, Linux, and macOS. **The deliverable is a usable Oracle environment, not an educational application.**
>
> **Scope freeze:** NO exercises, assignments, sample datasets, sample schemas, course content, DBA dashboard, SQL editor, web terminal, custom remote backend, student/teacher accounts, public REST API, or learning-management features. V1 includes one narrow exception: `oralab ui`, a localhost-only CPU/RAM and lifecycle companion embedded in the OraLab binary. It must never replace native Oracle terminal administration.

## 1. Mission

Give a student or professor on a supported computer a straightforward way to run **real Oracle Database 19c**, access the actual Linux shell and Oracle tools, and carry out normal **single-instance Oracle DBA and database optimization** tasks with the same native utilities used in ordinary Oracle installations.

The project does **not** implement DBA features itself: **Oracle supplies them**. Our work is to package an accurate, reproducible container configuration, persistence, simple lifecycle commands, sensible resource defaults, platform guidance, and verifiable operational documentation.

### Only five V1 product requirements

1. **Easy installation and startup.** Short, clear steps; Docker-based; few prerequisites; clear readiness and recovery information.
2. **Native command-line tools.** SQL*Plus, RMAN, Oracle Net and listener (`lsnrctl`), Data Pump (`expdp`/`impdp`), and the Linux shell; privileges sufficient for actual single-instance administration.
3. **Persistence.** Database files, required mutable configuration, and backups must survive normal stop/start and appropriate container recreation without losing work.
4. **Real DBA and optimization operations.** Oracle instance/CDB/PDB, users, tablespaces, SQL tuning, explain/actual execution plans, memory configuration, networking, backups and recovery using Oracle's own tools. Do not build wrappers that mimic these features.
5. **Configurable student-laptop resources.** CPU/memory/container shared memory and supported database initialization memory parameters; honest minimum hardware guidance.

## 2. Hard exclusions — do not implement

- No general-purpose frontend, SQL/DBA browser UI, database proxy, authentication portal, custom RBAC, admin dashboard, Electron app, or IDE extension. The only browser surface is the embedded, localhost-only OraLab resource manager for Docker CPU/RAM limits, status, usage and necessary lifecycle operations.
- No exercises, tutorials masquerading as exercises, seeded educational schemas, synthetic datasets, SQL exercise files, automated grading, TP packages, or professor content-management features.
- No Kubernetes, monitoring stack, metrics dashboard, Prometheus, Grafana, unnecessary sidecars, external cache, or orchestration platform.
- No replacement database (PostgreSQL, Oracle XE, 21c, 23ai/26ai), unless the owner explicitly approves a change of scope. **Oracle 19c is mandatory.**
- No RAC, ASM/Grid Infrastructure, full Data Guard topology, or host-kernel administration as V1 promises. These are not part of a simple, single-container lab.
- No automatic destruction/reset of user databases or volumes, no silent image upgrades, and no destructive scripts included in the normal workflow.
- No redistribution/committing Oracle software binaries, Oracle database image tarballs, or confidential credentials.

## 3. User experience — the primary product

The first-use experience should be this straightforward:

1. Student installs an appropriate Docker runtime and obtains access to a **legitimately licensed, architecture-compatible Oracle 19c image**.
2. Student downloads/clones this small configuration repository.
3. Student runs `oralab setup`, selects image and local password/initial resource settings, and performs any required Oracle registry sign-in/terms acceptance.
4. Student runs `oralab up` to launch the instance. OraLab talks directly to the Docker Engine API and behaves consistently on Windows, Linux and macOS.
5. Student follows live logs or checks the verified health/readiness mechanism; no fixed “sleep 90 seconds” guesswork.
6. Student enters the running container and uses `sqlplus / as sysdba`, or uses `oralab exec -- sqlplus / as sysdba` directly.
7. Student uses `rman target /`, `lsnrctl status`, Data Pump, and SQL queries normally.
8. Student stops the environment gracefully. Starting it again later shows the **same database**, not a new one.

**Host-terminal vs container-shell vs SQL*Plus vs RMAN must always be distinguished in documentation.** Terminal-first means no GUI needed beyond the user's existing Docker environment. Docker Desktop may have its own vendor UI, but users do not need to use it to operate Oracle.

### Canonical command interface

Host terminal:

```text
oralab doctor
oralab setup
oralab up
oralab logs
oralab shell
oralab exec -- sqlplus / as sysdba
oralab stop
oralab start
```

Inside the Oracle container (use the correct OS user as required by the image):

```sh
sqlplus / as sysdba
rman target /
lsnrctl status
```

Inside SQL*Plus (NOT the host shell):

```sql
SELECT instance_name, status FROM v$instance;
SHOW PDBS;
SELECT name, open_mode FROM v$database;
```

OraLab manages only setup, Docker lifecycle, readiness, logs and container access. It must not rename, reimplement or imitate Oracle utilities. `oralab exec --` passes the native command and arguments to the Oracle container unchanged. The agent must verify commands against the **actual selected image**, including whether `bash` and local OS SYSDBA authentication work as documented. These are examples of the expected user experience, not a claim that an untested image will behave identically.

## 4. Architecture and technology choices

### 4.1 Core architecture

- **Container runtime:** Docker Engine on Linux, Docker Desktop with WSL 2 on Windows, Docker Desktop or supported compatible Linux-container runtime on macOS.
- **Container control:** the supported Moby Go client talks directly to Docker Engine with automatic API negotiation. OraLab owns one `oralab-oracle` container and its named volumes; Docker Compose is not required.
- **Universal host CLI:** one small Go codebase compiled to standalone `oralab` executables for Windows, Linux and macOS. End users require no Go installation or Docker CLI. OraLab uses typed Engine API calls and transparently passes native Oracle commands; it is not a custom DBA interface.
- **Local resource UI:** `oralab ui` serves embedded HTML/CSS/JavaScript on `127.0.0.1:8765` by default. It reads actual Docker capabilities, effective container limits and live statistics; it changes only the Oracle container CPU/RAM limits and performs confirmed lifecycle operations. It exposes no SQL, terminal, arbitrary-command or volume-deletion endpoint.
- **Image:** official Oracle Container Registry 19c image, **or** an image built locally from Oracle's official `oracle/docker-images` SingleInstance recipes and separately obtained Oracle 19c binaries. No unverified third-party Oracle images.
- **Inside container:** Oracle Database 19c on Linux, listener, SQL*Plus, RMAN and the native included administration utilities.
- **Persistence:** a named Docker volume mounted at the image's documented `/opt/oracle/oradata` (or the selected image's verified equivalent), and a clearly separated persistent backup location. Preserve additional mutable configuration only where needed; verify paths first.
- **Network:** Oracle listener port 1521 bound to **localhost by default**, not `0.0.0.0`; no optional database web-management port unless strictly needed (it is not needed in V1).
- **Resources:** configurable Docker memory/CPU and `/dev/shm`, with supported first-initialization Oracle `INIT_SGA_SIZE` and `INIT_PGA_SIZE` parameters where image-compatible.

### 4.2 Portability reality

| Host | Intended mechanism | V1 status rule |
|---|---|---|
| Windows x86-64 | Docker Desktop + WSL 2 + Linux AMD64 Oracle image | Must be validated |
| Linux x86-64 | Docker Engine + Linux AMD64 Oracle image | Must be validated |
| Intel macOS | Docker Desktop Linux VM + Linux AMD64 Oracle image | Must be validated |
| Apple Silicon macOS | Linux ARM64 container image, **if an actual Oracle 19c ARM64 build is confirmed compatible** | Validate separately; document limitations rather than assume parity |
| Linux ARM64 | ARM64 Oracle 19c image, if available and validated | Optional additional target; no fabricated support claim |

**Important:** WSL 2 is Windows-only. macOS uses a Linux-container virtualization layer, not WSL. Image compatibility is **architecture-specific**; simply writing `platform: linux/amd64` on an ARM Mac does not guarantee Oracle will run reliably under emulation.

Oracle's official SingleInstance documentation lists Oracle Database 19c x86-64 builds and Linux ARM64 support for 19c Enterprise Edition via separately supplied ARM64 binaries. This establishes a path for investigation, **not proof that every Oracle Container Registry tag runs on both architectures**. Investigate actual image manifests/build options, supported editions, minimum requirements, and tool compatibility, and record tested facts.

### 4.3 Licensing and distribution

- **Do not distribute Oracle binaries or prebuilt proprietary database images in this repository.** Point users to authorized official Oracle download/registry paths.
- Distinguish software source availability from permission for **classroom or third-party training**. Developer/test rights do not automatically establish distribution and educational teaching rights; document Oracle's applicable license terms and potential Oracle Academy/institutional options for users to confirm.
- Clearly distinguish Oracle Enterprise features and potentially separately licensed management/tuning features from basic SQL tuning via standard views/SQL*Plus. Do not imply every Enterprise add-on is freely available.
- This repository's own files may have their own open-source license, which does **not** license Oracle Database itself.

## 5. Repository — minimal by design

Target layout (trim any file with no proven purpose):

```text
oracle19c-dba-lab/
├── PROJECT_PLAN.md
├── README.md
├── go.mod
├── go.sum
├── cmd/
│   └── oralab/
│       ├── main.go
│       ├── docker_engine.go
│       ├── resource_manager.go
│       ├── ui_server.go
│       ├── main_test.go
│       ├── docker_engine_test.go
│       └── web/             # Embedded, dependency-free local UI
├── .env.example
├── .gitignore
├── LICENSE                  # Only our configuration/documentation
└── docs/
    ├── compatibility.md    # Precisely which image/OS/architecture was tested
    ├── configuration.md    # Environment variables and first-boot semantics
    ├── dba-cli.md          # How to access native tools; command contexts
    ├── oralab.md           # Universal lifecycle CLI; native-command passthrough
    ├── persistence.md      # Data, config and backup storage behavior
    ├── troubleshooting.md  # Docker/Oracle/common terminal problems
    └── licensing.md        # Official image access and legal cautions
```

Do **not** create `labs/`, `exercises/`, `datasets/`, `samples/`, a web app, or a separate service. OraLab is the sole project-specific command interface and must remain a thin Docker Engine lifecycle adapter. End users download a standalone native binary; they do not install Go, Bash, Make, Python, Node or Java to launch Oracle. Contributors may use Go to build and test OraLab.

## 6. Implementation phases and agent tasks

### Phase 0 — Verify official Oracle 19c prerequisites BEFORE finalizing the container specification

**Tasks**

- [ ] Read Oracle's official SingleInstance container README, image/registry instructions, and the Oracle 19c/ARM64 platform notes.
- [ ] Select exact **19c** base image(s) for AMD64, and a separate ARM64 plan if verifiable; record tag/digest, edition, update/RU, exposed ports, environment variables and installed CLI binaries.
- [ ] Choose the simplest image acquisition method: first prefer an officially hosted eligible image; otherwise document how to build locally using Oracle's official recipes and user-obtained licensed Oracle installers.
- [ ] Confirm access prerequisites: Docker version, memory/disk, registry account, terms acceptance, optional host virtualization/WSL 2.
- [ ] Inspect and record Oracle container initialization/startup behavior, signal handling, readiness marker, expected Linux user, and database file path.
- [ ] Confirm which initialization environment variables work with the chosen image (e.g. `ORACLE_SID`, `ORACLE_PDB`, `ORACLE_PWD`, `INIT_SGA_SIZE`, `INIT_PGA_SIZE`, `ENABLE_ARCHIVELOG`), and any edition-specific limits.
- [ ] Confirm whether `sqlplus`, `rman`, `lsnrctl`, `expdp`, `impdp` exist in the target image. If missing, resolve through legitimate Oracle tooling/image selection rather than simulating a tool.
- [ ] Record **verified vs unverified** support per OS and architecture. Do not label a platform tested without running the actual database there.

**Exit condition:** `docs/compatibility.md` lists a reproducible official image path, verified prerequisites, real constraints, and clear platform statuses (`PASS`, `FAIL`, `NOT TESTED`, `BLOCKED`). No unsupported promises.

### Phase 1 — Build a simple, repeatable startup configuration

**Tasks**

- [x] Implement one directly managed container named `oralab-oracle` through the supported Moby client modules.
- [ ] Build and test the standalone `oralab` CLI for Windows AMD64, Linux AMD64/ARM64, and macOS AMD64/ARM64 from one Go codebase.
- [ ] Limit OraLab to `doctor`, `setup`, `up`, `status`, `logs`, `shell`, `exec --`, `start`, `stop`, `restart`, `ui`, `version` and `help`. Do not add reset, destroy, prune or volume-deletion commands.
- [ ] Ensure `oralab exec -- <command>` preserves native command names, arguments, interactive streams and exit status. Oracle commands remain `sqlplus`, `rman`, `lsnrctl`, `expdp` and `impdp`.
- [ ] Bind `oralab ui` to IPv4 loopback only, reject untrusted hosts/origins, protect state changes with a session token, and expose fixed allowlisted operations rather than arbitrary Docker or shell commands.
- [ ] Parameterize selected Oracle image without an automatic `latest` tag. Document how to choose a correct architecture-specific image if required.
- [ ] Mount the persistent Oracle data volume at the **verified** database path (typically `/opt/oracle/oradata` for Oracle's 19c SingleInstance image).
- [ ] Mount a separate persistent volume for RMAN backups and exports, **only after verifying access, permissions and correct location**. Backups must remain separate from the main data volume.
- [ ] Set listener bind to `127.0.0.1:1521:1521` by default. Explain the choice and provide an optional deliberate host-port override only if it remains safely bound to localhost.
- [ ] Set adequate `shm_size`; do not depend on Docker's small default `/dev/shm`.
- [ ] Configure graceful stop and a restart policy that does not undo deliberate Oracle DBA shutdown exercises. Document that database shutdown and container shutdown are different operations.
- [ ] Add `.env.example` with safe placeholders and understandable defaults, but **no real passwords**, tokens or registry credentials. Ignore `.env` and any actual secrets in Git.
- [ ] Define a straightforward first-run password setup. Document exposure limitations of ordinary container environment variables and prefer safer, image-supported alternatives if truly compatible and simple.
- [ ] Detect readiness using the selected image's genuine healthcheck or reliable documented logs. Do not use arbitrary delays or generic SQL tests against the wrong service.
- [ ] Do not enable/initialize optional Oracle features simply to create extra functionality. Explain that some first-initialization variables do not retroactively modify an existing persisted database.
- [ ] Test first initialization and a normal stop/start of **the same database**.

**Exit condition:** A supported user follows README setup; `oralab up` starts a real Oracle 19c database through Docker Engine and it reaches a verifiable ready state. A second start reuses existing data. The exact Engine API container specification and volume identity remain documented for diagnosis and recovery.

### Phase 2 — Validate the authentic command-line DBA experience

No custom DBA interface. Validate the Oracle tools directly.

**Tasks**

- [ ] Shell entry: `oralab shell`, user and `ORACLE_HOME`/`ORACLE_SID` environment inspection.
- [ ] SQL*Plus local privileged connection: `sqlplus / as sysdba`.
- [ ] Instance identity and state: `v$instance`, `v$database`, database version/component/edition inspection.
- [ ] Multitenant: inspect CDB/PDB and switch containers appropriately; confirm relevant operations are available.
- [ ] Instance administration: demonstrate ability to issue appropriate `STARTUP`/`SHUTDOWN` commands with the correct privileges; verify how this interacts with container lifecycle.
- [ ] Storage: verify DBA can create/manage a tablespace and add datafiles **within persistent storage**; confirm created files do not disappear upon container replacement.
- [ ] Security: verify real Oracle user/role/privilege administration, not a project-level user system.
- [ ] Networking: verify `lsnrctl status`, service registration and `sqlplus` connections through the local listener.
- [ ] RMAN: verify CLI entry, `SHOW ALL`, configured backup destination and actual backup to the separate persistent volume; perform read-only backup validation. Carry out any destructive recovery test **only on an expendable test volume created by the agent, not on user data**.
- [ ] Data Pump: verify `expdp` and `impdp` binaries and that an explicitly created, properly granted Oracle DIRECTORY maps to persistent accessible storage; do not ship data or educational sample schemas.
- [ ] Optimization: verify standard Oracle query plans, `EXPLAIN PLAN`, `DBMS_XPLAN`, `DBMS_STATS`, indexing and regular execution/statistics inspection are usable with suitable privileges. **Do not create teaching datasets, guided exercises or sample SQL files**; these are capability checks, not project features.
- [ ] Logs and diagnostics: identify alert-log location and demonstrate how a DBA reads Oracle diagnostics from the container.
- [ ] For every test, record actual result and platform/version. If a utility is optional or absent, describe it accurately instead of claiming success.

**Exit condition:** `docs/dba-cli.md` concisely explains native commands and shows which tool runs in which terminal context. A capability matrix records verified support for SQL*Plus, RMAN, listener, Data Pump, CDB/PDB and optimizer tooling.

### Phase 3 — Prove reliable persistence and backup storage

**Mandatory persistence tests** (use agent-created disposable state for tests, not actual student data):

| Case | Action | Expected result |
|---|---|---|
| P1 | Create disposable test object in database; stop and start container | Object still exists |
| P2 | Restart Docker engine or host (where controllable) | Same Oracle database and object remain |
| P3 | Remove/recreate only the managed container through a disposable test harness while retaining `oralab-oradata` | Same database and object remain |
| P4 | Delete/recreate the container **without** deleting named volumes | Same database and object remain |
| P5 | Create RMAN backup in separately mounted backup path; recreate container | Backup files remain |
| P6 | Create legitimate persistent, test-owned mutable configuration; recreate container | Required config remains, or documented, non-destructive startup recreation restores equivalent behavior |
| P7 | Create a datafile/tablespace in authorized persistent Oracle storage; recreate container | No missing-datafile error; data remains |

**Tasks**

- [ ] Verify ownership/write permissions and volume semantics on Docker Desktop and Linux.
- [ ] Confirm exactly which Oracle database files, password files, network settings, SPFILE, archive/FRA and diagnostic artifacts live within persistent storage in the chosen image. **Do not assume mounting `/opt/oracle/oradata` preserves every file under `$ORACLE_HOME`**.
- [ ] Choose the smallest viable solution for other mutable required configuration: persist designated directories or reproducibly regenerate image-supported non-data configuration on startup. Avoid blindly mounting over Oracle software directories.
- [ ] Preserve backup files independently of live data; explain that a Docker volume alone is **not** a database backup and that RMAN backups should be kept on independent storage for actual resilience.
- [ ] Document the fixed `oralab-oradata` volume identity, especially if users rename the repository or change machines.
- [ ] Document backup storage consumption, archived redo growth, and disk-space planning.
- [ ] Never call the Engine volume-remove/prune endpoints as a routine action. Any reset/deletion instructions must be visibly separated as **dangerous** and require manual user decision.

**Exit condition:** All feasible persistence tests show `PASS` with evidence. Any untested host reboot test is explicitly `NOT TESTED`; no false claims.

### Phase 4 — Make it easy on ordinary student/teacher computers

**Tasks**

- [ ] Test a **16 GB host RAM** laptop reference configuration where possible. Select evidence-based starting memory/CPU limits; do not market unsupported numbers as “minimum.”
- [ ] Expose simple, accurately named configuration for container RAM, CPU quota/count, SHM and Oracle first-initialization memory. Explain independent memory constraints and shared-memory requirements.
- [ ] Explain that image-provided `INIT_SGA_SIZE`/`INIT_PGA_SIZE` variables take effect **when the database is initially created**; changing `.env` alone generally will not change memory settings in an already persisted database. Describe a safe DBA SQL/SPFILE-based adjustment workflow instead of recreating/destroying data.
- [ ] Preserve reasonable headroom for Docker Desktop, the host operating system, and the database. Do not recommend limits based only on optimistic assumptions.
- [ ] Document actual first-start vs later-start times only after measurement, rather than promising “instant startup.”
- [ ] Make setup instructions host-specific **only where necessary**: WSL 2 prerequisites for Windows, Linux group/permission requirements, Docker Desktop VM resource settings on macOS, Apple Silicon image selection and ARM64 limitations.
- [x] Use the Docker Engine Go API so lifecycle behavior is shell-neutral on every host.
- [ ] Explain how to stop the Oracle container gracefully and restart it using simple consistent commands.
- [ ] Make `oralab ui` display host OS, Docker availability/capacity, Oracle container state and database readiness without confusing Docker's Linux VM with the host OS.
- [ ] Display configured and effective CPU/RAM limits plus lightweight live usage from Docker; show unavailable data honestly rather than invented zeros.
- [ ] Validate CPU/RAM changes against Docker-reported capacity, persist only `ORACLE_CPUS` and `ORACLE_MEMORY`, preserve unrelated `.env` values and credentials, and verify Docker's effective limits after application.
- [x] Apply supported CPU/RAM updates live through Docker's container-update endpoint. If a future change requires interruption, require explicit confirmation first. Never change Oracle SGA/PGA/MEMORY parameters, global Docker Desktop/WSL settings, or persistent volumes.

**Exit condition:** A first-time student can follow README without building scripts, installing programming toolchains, or knowing Oracle internals beforehand. If downloading licensed Oracle software requires extra action, explain it instead of hiding it.

### Phase 5 — Cross-platform validation and finishing documentation

**Tasks**

- [ ] Windows AMD64 + Docker Desktop WSL 2: actual Oracle initialization and CLI/persistence validation.
- [ ] Linux AMD64 + Docker Engine: same.
- [ ] Intel macOS + Docker Desktop: same (if test hardware available).
- [ ] Apple Silicon macOS: verify native ARM64 Oracle 19c option and real startup/tooling, **or explicitly label `NOT TESTED`/`BLOCKED` with the reason**. Do not conflate x86 emulation with native support.
- [ ] Confirm the typed Engine API container specification against the selected image, checking that no secrets are printed in CI/log outputs.
- [ ] Confirm `.env`, Oracle data/backup contents and proprietary binaries cannot be accidentally committed.
- [ ] Review README from a new-user perspective: no hidden commands, assumed paths, obsolete tags, unknown permissions or misleading “works on everything” claims.
- [ ] Include a very short troubleshooting section: image pull/registry terms, startup not ready, ORA connection errors, SHM/memory failures, Linux volume permissions, Apple Silicon image mismatch, full backup volume, Docker project/volume mismatch.

**Exit condition:** Published compatibility table explicitly distinguishes tested platforms from targets awaiting validation. Documentation is reproducible and makes no unverifiable performance or support promises.

## 7. Docker Engine API container requirements (implementation contract)

OraLab should implement the simplest valid direct container configuration meeting these conditions:

1. Exactly **one Oracle database service**.
2. Image is explicitly configurable and 19c-specific; no silent upgrade to another major version.
3. Documented database name/PDB name/password initialization variables are used only if supported by the selected image.
4. Bind network listener to `127.0.0.1` by default.
5. Named persistent database volume at the correct, verified path.
6. Independently persistent backup destination; ensure Oracle user permissions.
7. Configurable `shm_size`, sensible validated memory/CPU constraints, and adequate shutdown grace period.
8. No unnecessary Linux capabilities or `privileged: true` by default.
9. Robust, image-supported readiness strategy; start/stop that does not erase data.
10. No additional container, general-purpose frontend, schema initialization scripts, exercise folders or synthetic data creation. The embedded `oralab ui` host utility is allowed and is not a container.
11. Use the supported `github.com/moby/moby/client` and `github.com/moby/moby/api` modules with API negotiation; do not parse Docker CLI text or shell out to Compose.

**Design trade-off:** Native DBA tools require administrative freedom **inside the disposable Oracle container**. Do not achieve this by granting unsafe unrestricted access to the host. Explain differences between Oracle SYSDBA rights and host root/admin rights.

## 8. Configurability and first-run semantics

Document each supported `.env` setting in a concise table with name, purpose, example, default, whether it is initial-create-only, and whether it can be changed later without rebuilding the database. At a minimum evaluate:

- Image reference / architecture selection.
- Oracle SID and PDB name.
- Initial SYS/SYSTEM/PDB admin password setup (never a real default password committed).
- Host-local listener port.
- Container memory limit and CPU allocation.
- `/dev/shm` size.
- SGA and PGA initialization sizes (initial creation only if supported).
- ARCHIVELOG initial configuration, if appropriate; do not misrepresent as automatically changeable after initialization.
- Project/volume name considerations.

**Important operational rule:** Changing a Docker environment variable cannot be assumed to rewrite an existing Oracle database SPFILE, datafile structure, PDB name, or edition. Where a setting is a database-administration operation, document that Oracle itself must be configured using the appropriate Oracle tools and that changes should be backed up.

## 9. Documentation acceptance contract

### `README.md` — practical first page

Must answer, in this order:

1. What this is (genuine terminal Oracle 19c DBA environment) and what it is not (not a web application).
2. Actual host prerequisites and edition/architecture caveats.
3. How to get the official image lawfully and accept registry terms where applicable.
4. Two-to-five concise steps to configure and start using OraLab through Docker Engine (plus unavoidable official Oracle image acquisition).
5. How to know when Oracle is ready.
6. How to open the optional localhost resource UI and how to connect as SYSDBA and open shell/RMAN/listener utilities.
7. How to stop/restart **without losing data**.
8. Where data/config/backups persist and what **not** to delete.
9. Links to CLI, configuration, platform support, licensing, troubleshooting docs.

### `docs/dba-cli.md`

A **tool access reference**, not a workbook. Include how to launch SQL*Plus, RMAN, `lsnrctl`, `expdp`, `impdp` and use Oracle's administrative shell; annotate whether a command belongs to host terminal, container shell, SQL*Plus or RMAN. Include common built-in status/inspection commands and a brief explanation of privileges. Do not author learning exercises or supply data.

### `docs/persistence.md`

Explain exactly which volumes are attached where, the difference between stop/recreate/delete-container/delete-volume operations, how backups remain available, and any mutable config exceptions. State clearly that named volumes are not a substitute for RMAN backup and off-host backup for important work.

### `docs/compatibility.md`

Include an **honest test matrix** (host platform, CPU architecture, image tag/digest, DB version/RU, Oracle edition, available native utilities, test date, readiness/persistence results). Distinguish `PASS` vs `NOT TESTED` vs `BLOCKED` vs `FAIL`.

## 10. End-to-end acceptance checklist

V1 is **DONE** only when the following are satisfied on every platform marked supported:

- [ ] Setup follows documented Docker/Oracle prerequisites with no unexpected programming framework.
- [ ] The same OraLab command names and behavior work on every platform marked supported; platform-specific binaries are built from the same source and require no language runtime.
- [ ] `oralab exec --` runs native Oracle commands without renaming or imitating them.
- [ ] `oralab ui` binds only to localhost, works without external services, reports real Docker state, and provides no SQL editor, web terminal or arbitrary-command endpoint.
- [ ] CPU/RAM changes require valid local Docker capacity, preserve unrelated configuration and volumes, use live Engine updates when supported, request confirmation before any interruption, and are reported successful only after effective limits are verified.
- [ ] Exact **Oracle Database 19c** version and edition verified via the running database.
- [ ] Actual readiness is observable, not guessed by a fixed wait.
- [ ] User can enter the Linux container shell from terminal.
- [ ] `sqlplus / as sysdba` works with the documented user and environment.
- [ ] Real instance/CDB/PDB inspection and privileged administrative operations work.
- [ ] `rman target /` works; an actual RMAN backup can be stored persistently.
- [ ] `lsnrctl status` works; service is reachable on documented localhost port.
- [ ] Data Pump binaries work and a persistent directory can be configured.
- [ ] Database optimization commands (`EXPLAIN PLAN`, `DBMS_XPLAN`, `DBMS_STATS`) are available and documented as native Oracle features.
- [ ] Oracle data and a test-created object survive stop/start and container recreation with volumes retained.
- [ ] Backup files survive container recreation independently of main Oracle data.
- [ ] Configuration persistence limits are resolved or accurately documented.
- [ ] Host resource settings are configurable; first-initialization-only parameters are clearly identified.
- [ ] Windows/Linux/macOS coverage is documented with truthful validation status; ARM64 is not falsely claimed to work.
- [ ] Repository contains no credentials, proprietary binaries, GUI, lesson/exercise files or datasets.
- [ ] Licensing information explicitly explains end users/institutions must have appropriate Oracle rights.

## 11. Instructions to the coding agent — execution behavior

1. **Keep scope exactly V1.** Never add exercises, datasets, sample schemas, dashboards, frontend, backend, or nonessential tools.
2. **Research and verify before assuming.** Use official Oracle documentation and inspect the real image when available. Source and record architecture/version/volume/variable details.
3. **Implement in order:** image compatibility → minimal Engine API container specification → CLI access → persistence and backups → resource config → cross-platform validation → concise documentation.
4. **Prefer fewer files and fewer commands.** OraLab is a thin lifecycle interface over Docker Engine, not a replacement for Docker or Oracle utilities. The project should be understandable to a student who knows little Docker.
5. **Never silently delete data.** Treat student data and named Docker volumes as important. Any validation that creates/deletes Oracle objects must use clearly isolated temporary test objects/volumes on the agent's own instance, without shipping them as product data.
6. **Report exactly what you actually tested.** If Oracle binaries, registry access, licensing acceptance, Docker runtime or a host OS are unavailable, do the configuration/documentation work you can and explicitly mark validation as `NOT TESTED` or `BLOCKED`. Do not fake test output or claim cross-platform compatibility based only on syntactic YAML validation.
7. **Do not switch Oracle version as a shortcut.** Ask the project owner before changing the explicit Oracle 19c requirement.
8. **Keep the browser boundary narrow.** `oralab ui` manages only local Docker resource limits, status, usage and lifecycle. Never expand it into Oracle administration, SQL execution, a web terminal, backup management, storage deletion, accounts or remote access.
9. **Do not stop at a proposal.** Create the V1 repository files, configuration, and documentation; run the applicable tests in the agent's actual environment; then report artifacts and unresolved blockers honestly.

### Expected agent completion report

Report concisely:

- Files created/modified.
- Official Oracle 19c image path, edition, version, and architecture chosen.
- The one command to start the environment and the command to enter Oracle as SYSDBA.
- Results of startup, native tool, persistence, backup, and resource tests (`PASS`, `FAIL`, `NOT TESTED`, `BLOCKED`).
- Windows/Linux/Intel Mac/Apple Silicon status separately.
- Known prerequisites and licensing issues.
- Outstanding **V1-only** blockers, if any.

---

## Official reference starting points

- Oracle official single-instance container instructions: <https://github.com/oracle/docker-images/tree/main/OracleDatabase/SingleInstance>
- Oracle official container registry: <https://container-registry.oracle.com/>
- Oracle Database documentation: <https://docs.oracle.com/en/database/oracle/oracle-database/19/>
- Oracle Database 19c Linux ARM64 download information: <https://www.oracle.com/database/technologies/oracle19c-linux-arm64-downloads.html>
- Oracle Academy: <https://academy.oracle.com/>

**Final principle:** The product is a **minimal, portable, persistent Oracle 19c server you can genuinely administer from a terminal**. Everything beyond making that real experience accessible is outside V1.
