# MG-293 — Must-Gather Bundle Obfuscation

**Jira:** [MG-293](https://issues.redhat.com/browse/MG-293)  
**Epic:** [MG-297 — Integrate must-gather-clean with must-gather-operator](https://issues.redhat.com/browse/MG-297)  
**Type:** Story  
**Status:** Closed (Done)

## Summary

Integrate [must-gather-clean](https://github.com/openshift/must-gather-clean) into the must-gather-operator to enable automatic obfuscation of sensitive data (IP addresses, MAC addresses, Kubernetes Secrets, ConfigMaps) in must-gather bundles before they are uploaded to Red Hat support cases. When a user configures `obfuscate.enabled: true` on a MustGather CR, the operator runs must-gather-clean as a post-gather, pre-upload step inside the existing upload container, requiring no new images, containers, or sidecar coordination. Users can optionally provide a custom obfuscation config via a ConfigMap reference (`obfuscate.obfuscationConfigRef`) to tailor redaction rules to their needs.

## Motivation

When customers submit must-gather bundles to Red Hat Support, those bundles contain cluster state that may include sensitive information: IP addresses revealing network topology, MAC addresses identifying hardware, Secrets containing credentials, and ConfigMaps with internal configuration. Customers in regulated industries (finance, healthcare, government) are often unable or reluctant to share this data, even with their support provider.

Today, obfuscation requires a separate manual step using the `must-gather-clean` CLI tool after collecting the bundle. This creates friction in the support workflow and means most bundles are uploaded without any redaction.

### User Stories

- As an OpenShift cluster administrator, I want to set `obfuscate.enabled: true` on my MustGather CR so that sensitive data is automatically redacted before the bundle is uploaded to my Red Hat support case.
- As a security engineer in a regulated industry, I want to ensure that IP addresses, MAC addresses, Secrets, and ConfigMaps are removed or anonymized from diagnostic bundles so that our organization's network topology and credentials are not exposed to external parties.
- As a Red Hat support engineer, I want obfuscated bundles to use consistent replacements so I can still correlate events across resources (e.g., `x-ipv4-0000001-x` always refers to the same original IP within a bundle).
- As an OpenShift administrator, I want obfuscation logs included in the uploaded bundle so that I can audit what was redacted and verify compliance with my organization's data policies.
- As an OpenShift administrator, I want to obfuscate a previously collected must-gather bundle stored on a PVC without re-running a gather, so that I can redact sensitive data from existing bundles before sharing them.
- As an OpenShift administrator, I want to obfuscate an existing must-gather bundle on a PVC and automatically upload it to a Red Hat support case, so that I can redact and submit previously collected diagnostic data in a single step.
- As a security engineer, I want to provide a custom obfuscation config via a ConfigMap so that I can define organization-specific redaction rules (e.g., obfuscate only IPs but keep ConfigMaps, or add domain-specific patterns) without building a custom operator image.

### Goals

1. Enable one-step gather-and-obfuscate-and-upload via the `obfuscate` config on the MustGather CR (gather → obfuscate → upload)
2. Support obfuscation of existing bundles on a PVC without re-running a gather (obfuscate-only, via `obfuscate.source`)
3. Support obfuscation of existing bundles followed by upload without re-running a gather (obfuscate → upload, via `obfuscate.source` + `uploadTarget`)
4. Obfuscate sensitive data automatically before SFTP upload
5. Use consistent replacement so patterns remain debuggable (e.g., all occurrences of the same IP map to the same anonymized value)
6. Ship a safe default obfuscation config that omits Secrets and ConfigMaps, and consistently replaces IPs and MACs
7. Allow users to provide custom obfuscation rules via a ConfigMap reference (`obfuscationConfigRef`) to override the default config
8. Include obfuscation logs in the output bundle for auditability

### Non-Goals

1. Changes to the must-gather-clean library API (consumed as-is)
2. Domain-specific obfuscation patterns shipped as part of the default config (users provide these via `obfuscationConfigRef`)
3. Obfuscation progress reporting in the MustGather CR status

## Proposal

Add a new `obfuscate` field to `MustGatherSpec` that groups all obfuscation-related configuration:

- **`enabled`** (bool): Activates obfuscation.
- **`obfuscationConfigRef`** (object, optional): References a ConfigMap with custom obfuscation rules.
- **`source`** (object, optional): References an existing must-gather bundle on a PVC for obfuscation without running a new gather.

These sub-fields enable three supported operational modes:

| Mode | Fields Used | Description |
|------|-------------|-------------|
| Gather + Obfuscate + Upload | `obfuscate.enabled: true` + `uploadTarget` | Full pipeline: collect, redact, upload |
| Obfuscate Only | `obfuscate.enabled: true` + `obfuscate.source` | Redact an existing bundle from a PVC to a staging directory, no gather, no upload |
| Obfuscate + Upload | `obfuscate.enabled: true` + `obfuscate.source` + `uploadTarget` | Redact an existing bundle and upload it, no gather |

### Workflow Description

**Cluster administrator** is a human user responsible for collecting and uploading diagnostic data.

#### Mode 1: Gather + Obfuscate + Upload

This is the primary workflow for new must-gather collections with obfuscation.

1. The cluster administrator creates a MustGather CR with `spec.obfuscate.enabled: true` and an `uploadTarget` configured
2. The Must-Gather Operator creates a Job with gather and upload containers
3. The gather container runs `/usr/bin/gather`, writes output to the shared `/must-gather` volume, and appends `chown -R 65534:65534 /must-gather` before exiting (transferring ownership to the non-root upload container's uid)
4. The upload container detects gather completion via `pgrep` polling
5. The upload script checks the `obfuscate` environment variable
6. The operator binary is invoked with a separate output directory: `must-gather-operator obfuscate --input /must-gather --output /must-gather-upload/cleaned -v=3`
7. If `obfuscationConfigRef` is set, the referenced ConfigMap is mounted and used as the config; otherwise the built-in config is used
8. The obfuscation engine walks all files, applying the configured replacements and omissions, writing cleaned output to `/must-gather-upload/cleaned`
9. The upload script redirects `must_gather_output` to the cleaned directory (`must_gather_output="/must-gather-upload/cleaned"`)
10. An `obfuscation.log` is preserved in the cleaned output for auditability
11. The upload script proceeds to tar and SFTP upload from the cleaned directory
12. The MustGather CR status is updated to Completed

#### Mode 2: Obfuscate Only (No Gather, No Upload)

This mode allows administrators to redact an existing must-gather bundle stored on a PVC without re-collecting or uploading. Since the operator does not support updates to an existing MustGather CR, the administrator must create a **new** MustGather CR for the obfuscation run.

1. The cluster administrator has a previously collected must-gather bundle persisted on a PVC (e.g., from a prior MustGather CR with `spec.storage`)
2. The administrator creates a **new** MustGather CR with `spec.obfuscate.enabled: true` and `spec.obfuscate.source` referencing the PVC
3. The operator creates a Job with only an upload container (no gather container)
4. The upload container mounts the PVC at `/must-gather`
5. The operator binary is invoked with the appropriate config (custom or default)
6. Obfuscation reads from the PVC and writes cleaned output to `/must-gather-upload/cleaned`
7. The `obfuscation.log` is written to the cleaned output
8. The MustGather CR status is updated to Completed
9. The original bundle on the PVC remains untouched; the obfuscated bundle is in the staging directory for the administrator to retrieve

#### Mode 3: Obfuscate + Upload (No Gather)

This mode allows administrators to redact an existing bundle and upload it to a support case without re-collecting. As with Mode 2, a **new** MustGather CR must be created (the operator does not support spec updates on existing CRs).

1. The cluster administrator has a previously collected must-gather bundle on a PVC
2. The administrator creates a **new** MustGather CR with `spec.obfuscate.enabled: true`, `spec.obfuscate.source` referencing the PVC, and `spec.uploadTarget` configured
3. The operator creates a Job with only an upload container (no gather container)
4. The upload container mounts the PVC at `/must-gather`
5. Obfuscation runs on the bundle (using custom or default config)
6. The upload script proceeds to tar and SFTP upload
7. The MustGather CR status is updated to Completed

**Error case (all modes)**: If obfuscation fails (e.g., unreadable files, disk full), the upload container exits non-zero, the Job fails.

### API Extensions

One new field added to the existing `MustGatherSpec` in the `mustgathers.operator.openshift.io` CRD:

```go
// MustGatherSpec defines the desired state of MustGather
type MustGatherSpec struct {
    // ... existing fields ...

    // obfuscate configures post-gather obfuscation of sensitive data
    // (IPs, MACs, Secrets, ConfigMaps) before upload using must-gather-clean.
    // When obfuscate.enabled is true, the operator runs obfuscation on the
    // collected bundle before tarring and uploading.
    // +optional
    Obfuscate *ObfuscateConfig `json:"obfuscate,omitempty"`
}

// ObfuscateConfig configures the obfuscation behavior for a MustGather run.
type ObfuscateConfig struct {
    // enabled activates obfuscation of the must-gather bundle.
    // When true, the operator runs must-gather-clean on the collected or
    // referenced bundle before tarring and uploading.
    // +kubebuilder:default:=false
    // +optional
    Enabled *bool `json:"enabled,omitempty"`

    // obfuscationConfigRef references a ConfigMap in the operator namespace
    // containing a must-gather-clean configuration file.
    // The ConfigMap must have a key named "config.yaml" whose value is a
    // valid must-gather-clean obfuscation config.
    // If omitted, the operator uses the built-in default config which
    // consistently replaces IPs and MACs, and omits Secrets and ConfigMaps.
    // +optional
    ObfuscationConfigRef *corev1.LocalObjectReference `json:"obfuscationConfigRef,omitempty"`

    // source references an existing must-gather bundle on a PVC
    // for obfuscation without running a new gather.
    // When set, the operator skips the gather step and runs obfuscation
    // directly on the referenced PVC contents.
    // +optional
    Source *ObfuscateSourceConfig `json:"source,omitempty"`
}

// ObfuscateSourceConfig defines the source of an existing must-gather bundle
// to obfuscate without running a new gather.
type ObfuscateSourceConfig struct {
    // claim references the PersistentVolumeClaim containing the existing
    // must-gather bundle to obfuscate.
    // The PVC must be in the operator namespace.
    // +required
    Claim PersistentVolumeClaimReference `json:"claim"`

    // subPath is the path within the PVC where the must-gather bundle
    // is located. If omitted, the root of the PVC is used.
    // +optional
    SubPath string `json:"subPath,omitempty"`
}
```

No new CRDs. The `obfuscate` field is optional and defaults to `nil`, making this fully backward compatible.

#### Example CRs

**Mode 1: Gather + Obfuscate + Upload (built-in config)**

```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: MustGather
metadata:
  name: obfuscated-gather
  namespace: openshift-must-gather-operator
spec:
  serviceAccountName: must-gather-admin
  obfuscate:
    enabled: true
  uploadTarget:
    type: SFTP
    sftp:
      caseID: "02527285"
      caseManagementAccountSecretRef:
        name: case-management-creds
      internalUser: true
```

**Mode 1: Gather + Obfuscate + Upload (custom config)**

```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: MustGather
metadata:
  name: obfuscated-gather-custom
  namespace: openshift-must-gather-operator
spec:
  serviceAccountName: must-gather-admin
  obfuscate:
    enabled: true
    obfuscationConfigRef:
      name: my-obfuscation-rules
  uploadTarget:
    type: SFTP
    sftp:
      caseID: "02527285"
      caseManagementAccountSecretRef:
        name: case-management-creds
      internalUser: true
```

**Mode 2: Obfuscate Only (existing bundle on PVC, no gather, no upload)**

```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: MustGather
metadata:
  name: obfuscate-existing-bundle
  namespace: openshift-must-gather-operator
spec:
  serviceAccountName: must-gather-admin
  obfuscate:
    enabled: true
    obfuscationConfigRef:
      name: my-obfuscation-rules
    source:
      claim:
        name: my-mustgather-pvc
      subPath: "must-gather-2026-06-25"
```

**Mode 3: Obfuscate + Upload (existing bundle on PVC, no gather)**

```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: MustGather
metadata:
  name: obfuscate-and-upload
  namespace: openshift-must-gather-operator
spec:
  serviceAccountName: must-gather-admin
  obfuscate:
    enabled: true
    source:
      claim:
        name: my-mustgather-pvc
      subPath: "must-gather-2026-06-25"
  uploadTarget:
    type: SFTP
    sftp:
      caseID: "02527285"
      caseManagementAccountSecretRef:
        name: case-management-creds
```

#### Custom Obfuscation ConfigMap Example

The ConfigMap referenced by `obfuscationConfigRef` must contain a key `config.yaml` with a valid must-gather-clean config:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-obfuscation-rules
  namespace: openshift-must-gather-operator
data:
  config.yaml: |
    config:
      obfuscate:
        - type: IP
          replacementType: Consistent
          target: All
        - type: MAC
          replacementType: Consistent
          target: All
        - type: Regex
          regex: "my-internal-domain\\.corp\\.example\\.com"
          replacementType: Consistent
      omit:
        - type: Kubernetes
          kubernetesResource:
            kind: "Secret"
```

Users can customize which data types are obfuscated, add regex-based patterns for domain-specific content, and choose which Kubernetes resources to omit. The format follows the [must-gather-clean configuration schema](https://github.com/openshift/must-gather-clean#configuration).

### Topology Considerations

#### Hypershift / Hosted Control Planes

No unique considerations.

#### Standalone Clusters

No unique considerations.

#### Single-node Deployments or MicroShift

No unique considerations.

#### OpenShift Kubernetes Engine

No unique considerations.

### Implementation Details / Notes / Constraints

#### Architecture

Obfuscation runs as a post-gather, pre-upload step inside the existing upload container. No new containers, no new images, no new sidecar coordination.

```
                          Job Pod
┌────────────────────────────────────────────────────────┐
│                                                        │
│  gather container (uid 0)     upload container (uid    │
│                               65534)                   │
│  ┌──────────────────┐        ┌──────────────────────┐  │
│  │ /usr/bin/gather   │        │ 1. poll gather       │  │
│  │                   │        │ 2. obfuscate:        │  │
│  │ writes to         │ output │    read /must-gather  │  │
│  │ /must-gather ─────┼───vol──│    write to staging   │  │
│  │                   │        │    dir on upload vol   │  │
│  │ chown -R 65534    │        │ 3. tar /must-gather   │  │
│  │ /must-gather      │        │ 4. sftp upload        │  │
│  │ (when obfuscate)  │        │                       │  │
│  └──────────────────┘  ──vol──└──────────────────────┘  │
│                                                        │
│  Volumes:                                              │
│    output vol = /must-gather (emptyDir or PVC)         │
│    upload vol = /must-gather-upload (emptyDir,         │
│                 staging area for cleaned files + tar)   │
└────────────────────────────────────────────────────────┘
```

#### Volume Layout

| Volume | Mount Path | Type | Purpose |
|--------|------------|------|---------|
| `must-gather-output` | `/must-gather` | emptyDir or PVC | Gather container writes collected data; upload container reads for obfuscation |
| `must-gather-upload` | `/must-gather-upload` | emptyDir | Upload container workspace for staging cleaned output and tar archives |

#### Component Changes

1. **CRD types (`api/v1alpha1/mustgather_types.go`)** — Added `Obfuscate *ObfuscateConfig` struct to `MustGatherSpec`.
2. **Obfuscation logic (`main.go`)** — `runObfuscate` function invoked by upload script; accepts `--input`, `--output`, `--config`; uses `mgclean.Run()` with 4 parallel workers; writes `obfuscation.log`.
3. **Job template (`controllers/mustgather/template.go`)** — When `obfuscate.enabled`: gather appends `chown -R 65534:65534 /must-gather`; upload sets `obfuscate` env var; ConfigMap mount when `obfuscationConfigRef` set; omit gather when `obfuscate.source` set; PVC mount for source mode.
4. **Upload script (`build/bin/upload`)** — Obfuscation step before tar when `obfuscate=true`.
5. **Default obfuscation config (`build/obfuscate-config.yaml`)** — Baked into image at `/etc/must-gather-clean/default-config.yaml`.
6. **Dockerfiles** — Copy default config into image.
7. **Go module (`go.mod`)** — Added `must-gather-clean` dependency.

#### Permission Model

| Container | UID | Permissions |
|-----------|-----|-------------|
| gather | 0 (root) | Creates all files as root-owned in `/must-gather` |
| upload | 65534 (nobody) | Reads input from `/must-gather`, writes cleaned output to `/must-gather-upload` |

Resolved by `chown -R 65534:65534 /must-gather` in gather container when obfuscation enabled. Running upload as root was rejected (least privilege, restricted SCC).

#### Default Obfuscation Behavior

| Data Type | Action | Details |
|-----------|--------|---------|
| IPv4 addresses | Consistent replacement | `10.0.1.5` → `x-ipv4-0000001-x` |
| MAC addresses | Consistent replacement | `0e:a0:e7:92:3a:a3` → `x-mac-0000001-x` |
| Kubernetes Secrets | Omitted entirely | Files containing `kind: Secret` excluded |
| Kubernetes ConfigMaps | Omitted entirely | Files containing `kind: ConfigMap` excluded |
| Local IPs (127.0.0.1, 0.0.0.0, ::1) | Preserved | Not obfuscated |

#### Performance Impact

Obfuscation is CPU-bound. Benchmarks on OCP cluster (3 control-plane, 3 worker nodes):

| Workers | Bundle Size | Sensitive Data Density | Obfuscation Time |
|---------|-------------|------------------------|------------------|
| 4 | ~200 MB | Light | ~50 seconds |
| 4 | ~400 MB | Light | ~90 seconds |
| 8 | ~400 MB | Light | ~53 seconds |
| 8 | ~600 MB | Heavy | ~2 minutes |
| 8 | ~1.7 GB | Heavy | ~5 minutes |
| 8 | ~2 GB | Heavy | ~7 minutes |

Uses `automaxprocs` for cgroup CPU limits. Worker count hardcoded to 4 initially. Cleaned output requires temporary disk space equal to bundle size on upload volume.

### Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| `must-gather-clean` calls `klog.Exitf` on file errors | Upload fails hard | Runs in upload container, not controller; document fatal file errors |
| Obfuscation increases Job duration | Longer time to upload | CPU-bound, parallelized; see benchmarks |
| Container CPU limits lower than expected | Worker throttling | `automaxprocs` sets `GOMAXPROCS`; worker count revisitable |
| Omitted resources permanently excluded | Support may lack omitted data | Document omission behavior; custom config via `obfuscationConfigRef` |
| `obfuscate.source` requires additional disk space | Staging dir needs bundle-sized space | emptyDir sized for staging; original PVC untouched |
| Custom must-gather images may produce non-standard output | Obfuscation may miss files | Content-based regex; best-effort for custom images |
| Invalid custom ConfigMap | Runtime failure | Clear operator logs; `obfuscation.log` captures failure |
| CPU-intensive pod on control-plane/infra nodes | Starve critical components | Node affinity/tolerations schedule on worker nodes only |

### Drawbacks

- Adds `must-gather-clean` dependency, increasing binary size and vendor footprint
- Default ConfigMap omission is aggressive (mitigated by custom config)
- Custom config validation deferred to runtime

## Alternatives (Not Implemented)

- **Separate obfuscation container** — Rejected: no image exists, adds coordination complexity
- **Running upload container as root** — Rejected: least privilege, restricted SCC
- **Obfuscation in gather container** — Rejected: gather image lacks must-gather-clean
- **Inline obfuscation config in CR** — Rejected: ConfigMaps are standard pattern

## Open Questions

1. Should obfuscation be supported with custom must-gather images (best-effort vs validation)?
2. Should `obfuscate.enabled: true` without `uploadTarget` and without `obfuscate.source` be rejected via CEL validation?
3. Should obfuscation report (`report.yaml`) be surfaced in MustGather CR status?
4. Should `obfuscationConfigRef` ConfigMap be validated at CR admission time or only at Job execution?

## Test Plan

### Unit Tests

- `template_test.go`: Verify `chown` appended when `obfuscate.enabled` true
- `template_test.go`: Verify `obfuscate` env var when enabled
- `template_test.go`: Verify ConfigMap mount and `obfuscate_config` env when `obfuscationConfigRef` set
- `template_test.go`: Verify gather container omitted when `obfuscate.source` set
- `template_test.go`: Verify PVC mount for source mode
- Backward compatible with `nil` obfuscate parameter

### Integration Tests

- Verify `runObfuscate()` processes test bundle
- Verify output written to staging without modifying input
- Verify `obfuscation.log` present
- Verify consistent IP replacement across files

### E2E Tests

- **Mode 1**: gather + obfuscate + upload with SFTP verification
- **Mode 1 custom config**: ConfigMap with IP-only obfuscation
- **Mode 2**: obfuscate-only from PVC
- **Mode 3**: obfuscate + upload from PVC
- **Negative**: no obfuscation when omitted; rejection when source set but enabled false; Job failure on missing ConfigMap

## Graduation Criteria

### Tech Preview → GA

- All three modes working end-to-end
- Custom config via ConfigMap working
- Obfuscation logs in output bundles
- No upstream patches required
- Comprehensive test coverage including upgrade/downgrade
- Performance benchmarking for large bundles (>1GB)
- User-facing documentation in openshift-docs

## Upgrade / Downgrade Strategy

- **Upgrade**: Existing CRs unchanged; `obfuscate` defaults to nil
- **Downgrade**: Older operator ignores field; remove field before CRD downgrade

## Version Skew Strategy

Self-contained in operator. `must-gather-clean` vendored into binary.

## Operational Aspects

- Optional field; no impact on existing SLIs
- Failure modes: obfuscation failure → Job fails
- Resource consumption: temporary CPU/I/O increase in upload container

## Support Procedures

- Check upload container logs for obfuscation failures
- Disable by omitting `obfuscate` or setting `enabled: false`
- Common failures: permission denied (missing chown), `klog.Exitf` (corrupted files)

## Infrastructure Needed

None. Uses existing CI and SFTP upload target. `must-gather-clean` vendored as Go dependency.

## Future Enhancements

1. CR status phases: report `Obfuscating` between `Collecting` and `Uploading`
2. Obfuscation report in CR status
3. Gather + obfuscate without upload (persist to PVC only)
4. Upstream: replace `klog.Exitf` with returned errors in must-gather-clean

## Acceptance Criteria (Jira)

- Proposal is reviewed and approved.
