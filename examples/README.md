# MustGather example CRs

Sample `MustGather` custom resources for common operator workflows. Apply from any namespace where the CR and referenced secrets or PVCs exist.

## Obfuscation examples (MG-293)

Three obfuscation Job shapes are supported. Each example below is a valid CR that passes CRD validation (CEL rules FR-012/FR-013).

| Example file | Mode | Workflow | Spec scenarios |
|--------------|------|----------|----------------|
| [mustgather_obfuscate_default_upload.yaml](mustgather_obfuscate_default_upload.yaml) | **1** | Gather → obfuscate (default policy) → SFTP upload | SC-001, SC-002, SC-003 |
| [mustgather_obfuscate_custom_config.yaml](mustgather_obfuscate_custom_config.yaml) | **1** (custom policy) | Gather → obfuscate (ConfigMap policy) → SFTP upload | SC-004 |
| [mustgather_obfuscate_source_pvc.yaml](mustgather_obfuscate_source_pvc.yaml) | **2** | Obfuscate existing bundle on PVC → SFTP upload (no gather) | SC-005 |

**Mode 3** (obfuscate-only from source PVC, no upload) uses the same `obfuscate.source` shape as the source-PVC example but omits `uploadTarget`. Tech Preview: cleaned output stays on the Job staging volume; retrieval is documented in operator release notes.

### Cluster prerequisites

| Prerequisite | Mode 1 (default) | Mode 1 (custom) | Mode 2 (source PVC) |
|--------------|------------------|-----------------|---------------------|
| Service account with gather RBAC | Required | Required | Required (Job still runs upload/obfuscate pod) |
| SFTP case-management secret in CR namespace | Required | Required | Required when `uploadTarget` is set |
| ConfigMap in **operator namespace** (`must-gather-operator`) with key `config.yaml` | Not used (embedded default policy) | **Required** — name from `obfuscationConfigRef.name` | Not used unless `obfuscationConfigRef` is set |
| PVC with existing must-gather bundle in **same namespace as CR** | Not used | Not used | **Required** — name from `obfuscate.source.claim.name` |

Create supporting objects under [other_resources/](other_resources/) where applicable (namespace, service accounts, test PVC).

### Applying an example

```bash
oc apply -f examples/mustgather_obfuscate_default_upload.yaml
```

Replace `caseID`, secret names, PVC names, and ConfigMap names with values valid for your cluster.

## Non-obfuscation examples

| File | Purpose |
|------|---------|
| `mustgather_basic.yaml` | Minimal gather + upload |
| `mustgather_full.yaml` | Gather + upload with common options |
| `mustgather_with_pvc_subpath.yaml` | Persistent storage with subPath |
| `mustgather_timeout.yaml` | Custom gather timeout |
| `mustgather_retain_resources.yaml` | Retain Job/Pods after completion |
| `mustgather_with_since.yaml` / `mustgather_with_sincetime.yaml` | Audit log time filters |
| `mustgather_non_internal_user.yaml` | External Red Hat SFTP upload path |

Obfuscation is **not** enabled on these CRs (SC-007 backward compatibility).
