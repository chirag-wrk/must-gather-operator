# Deviations Observed

**Change**: mg-293
**Jira**: MG-293

---

## Phase 2 — Obfuscate library integration

- **Task T2_3**: CLI split across `cmd_root.go` and `cmd_obfuscate.go` for readability and testability (task listed `main.go` only).
- **Task T2_4**: Fixed `run.go` — cannot pre-create files in output dir before `cli.Run` (discovered by fixture test).

## Phase 3 — Job template obfuscation

- **Task T3_3**: ConfigMap volume mounts by name in Job pod namespace; cross-namespace copy from operator namespace deferred to Phase 6.

## Phase 5 — Default policy packaging

- **Task T5_5**: Used `ALLOW_DIRTY_CHECKOUT=true` due to uncommitted non-packaging files in working tree.

## Phase 6 — Controller validation and status

- **Task T6_3**: Whitespace-only `obfuscationConfigRef.name` treated as invalid (`InvalidConfigRef`) rather than skipped.
- **Task T6_5**: Added production validation for empty/whitespace `OPERATOR_IMAGE` (previously only missing var was rejected).
- **Task T6_6**: No new RBAC rules added — existing permissions already sufficient; verification and documentation only.

## Phase 7 — Test coverage and E2E

- **Task T7_3**: Ginkgo cluster suite not run — compile-only verification when no cluster available.
- **Task T7_4**: Cluster E2E not executed in working-folder environment; follows existing SFTP test skip pattern.
- **Task T7_5**: Isolation uses source PVC marker persistence and read-only subPath parity rather than gather-storage `must-gather.local.*` subdirectory pattern.
- **Task T7_6**: Uses source-PVC mode without `uploadTarget` to reach ConfigMap validation without SFTP pre-check.
- **Task T7_7**: Cluster Ginkgo specs not run locally; compile + fixture unit tests only. Manual SFTP fallback documented in test comments.
