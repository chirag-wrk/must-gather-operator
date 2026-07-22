# Feature Specification: Must-Gather Bundle Obfuscation

**Feature Branch**: `mg-293-must-gather-bundle-obfuscation`

**Created**: 2026-07-22

**Status**: Draft

**Input**: User description: "Integrate must-gather-clean into must-gather-operator to automatically obfuscate sensitive data in diagnostic bundles before upload to Red Hat support cases."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Gather, Obfuscate, and Upload in One Request (Priority: P1)

A cluster administrator collects a new must-gather diagnostic bundle and needs sensitive data redacted before it reaches Red Hat Support. They enable obfuscation on a single MustGather request that also specifies an upload destination.

**Why this priority**: This is the primary support workflow — most bundles are collected and uploaded in one step. Automating redaction here removes manual post-processing and closes the largest compliance gap.

**Independent Test**: Create a MustGather request with obfuscation enabled and an upload target; verify the uploaded bundle contains anonymized network identifiers, excludes configured sensitive resource types, and includes an audit log of redaction activity.

**Acceptance Scenarios**:

1. **Given** a cluster with the Must Gather Operator installed and a valid upload configuration, **When** the administrator creates a MustGather request with obfuscation enabled and no custom rules, **Then** the operator collects diagnostics, redacts sensitive data using the default policy, and uploads the cleaned bundle to the specified case.
2. **Given** a MustGather request with obfuscation enabled, **When** the collection and redaction complete successfully, **Then** the uploaded bundle contains no cleartext IP addresses or MAC addresses from the original cluster state, and each original value is replaced consistently throughout the bundle.
3. **Given** a MustGather request with obfuscation enabled, **When** the default policy is applied, **Then** Kubernetes Secret and ConfigMap content is omitted from the uploaded bundle while local loopback addresses remain unchanged.
4. **Given** a MustGather request with obfuscation disabled or omitted, **When** the operator processes the request, **Then** behavior is identical to the pre-obfuscation release with no redaction step.

---

### User Story 2 - Custom Organization Redaction Rules (Priority: P2)

A security engineer needs organization-specific redaction — for example, obfuscating IP addresses but retaining ConfigMaps, or adding domain-specific patterns — without maintaining a custom operator build.

**Why this priority**: Regulated customers require tailored policies. Configurable rules unlock adoption without forking the operator.

**Independent Test**: Provide a custom obfuscation policy via cluster configuration, create a MustGather request referencing it, and verify the uploaded bundle reflects the custom rules rather than the default policy.

**Acceptance Scenarios**:

1. **Given** a valid custom obfuscation policy stored in the cluster, **When** the administrator creates a MustGather request with obfuscation enabled and references that policy, **Then** redaction follows the custom rules instead of the default policy.
2. **Given** a custom policy that obfuscates IPs only (no MAC obfuscation, no resource omission), **When** the bundle is processed, **Then** IP addresses are consistently replaced, MAC addresses remain in cleartext, and Secrets/ConfigMaps are not omitted unless the custom policy specifies omission.

---

### User Story 3 - Obfuscate and Upload an Existing Bundle (Priority: P2)

An administrator previously collected a must-gather bundle to persistent storage and now needs to redact and upload it without re-running an expensive cluster-wide collection.

**Why this priority**: Re-collection is costly and disruptive. Redacting existing bundles supports delayed submission and compliance review workflows.

**Independent Test**: Pre-populate storage with an existing bundle, create a new MustGather request referencing that storage with obfuscation and upload enabled, and verify the uploaded bundle is redacted and the original stored bundle is unchanged.

**Acceptance Scenarios**:

1. **Given** an existing must-gather bundle on persistent storage, **When** the administrator creates a new MustGather request with obfuscation enabled, a source reference to that storage, and an upload target, **Then** no new collection runs, the bundle is redacted, and the cleaned result is uploaded.
2. **Given** a source-referenced obfuscation request, **When** processing completes, **Then** the original bundle on the source storage remains unmodified.

---

### User Story 4 - Audit What Was Redacted (Priority: P3)

An administrator or auditor must verify compliance with organizational data-handling policies after a bundle is processed.

**Why this priority**: Auditability builds trust with security teams and supports regulated-industry adoption.

**Independent Test**: Process any obfuscated bundle and confirm an obfuscation audit log is present in the output describing redaction activity.

**Acceptance Scenarios**:

1. **Given** a MustGather request with obfuscation enabled, **When** redaction completes successfully, **Then** the output bundle includes an obfuscation audit log documenting what was processed.
2. **Given** a successfully obfuscated bundle, **When** a support engineer reviews replacement patterns, **Then** the same original sensitive value maps to the same anonymized token throughout the bundle.

---

### User Story 5 - Obfuscate an Existing Bundle Without Upload (Priority: P3)

An administrator needs to redact a previously collected bundle stored on persistent storage without uploading it immediately — for example, to review the cleaned output before submission.

**Why this priority**: Supports internal compliance review before external sharing, but is secondary to the combined upload workflow.

**Independent Test**: Pre-populate storage with a bundle, create a MustGather request with obfuscation and source reference but no upload target, and verify a cleaned bundle is produced while the source remains untouched.

**Acceptance Scenarios**:

1. **Given** an existing must-gather bundle on persistent storage, **When** the administrator creates a MustGather request with obfuscation enabled and a source reference but no upload target, **Then** no collection runs and a cleaned bundle is produced without uploading.
2. **Given** an obfuscate-only request completes successfully, **When** the administrator accesses the cleaned output using documented operator procedures, **Then** the cleaned bundle reflects the configured redaction policy and the source storage is unchanged.

---

### Edge Cases

- **When** obfuscation is enabled but the request specifies neither a source reference nor an upload target, **then** the request is rejected with a clear validation error before any workload is created.
- **When** obfuscation is enabled with a source reference but obfuscation is explicitly disabled, **then** the request is rejected with a clear validation error.
- **When** a referenced custom obfuscation policy does not exist or is malformed, **then** the processing workload fails, the MustGather request reports failure, and no upload occurs.
- **When** redaction fails due to unreadable content or insufficient storage, **then** the processing workload fails with a non-zero exit, the MustGather request reports failure, and no partial upload is sent.
- **When** obfuscation is omitted or disabled on a MustGather request, **then** existing operator behavior is unchanged and bundles are uploaded without redaction.
- **When** the operator is upgraded on a cluster with existing MustGather requests that omit obfuscation, **then** those requests continue to behave as before without requiring modification.
- **When** the operator is downgraded while MustGather requests still specify obfuscation, **then** the older operator version ignores the obfuscation setting and processes requests without redaction, or API validation rejects unknown fields if the API schema was also downgraded.
- **When** a bundle was produced by a non-standard collection image, **then** obfuscation is attempted on a best-effort basis and the audit log records the outcome.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The operator MUST allow cluster administrators to enable bundle obfuscation on a MustGather request via an optional obfuscation configuration block.
- **FR-002**: When obfuscation is enabled on a new collection request, the operator MUST redact sensitive data from the collected bundle before any upload step executes.
- **FR-003**: When obfuscation is enabled with a source reference to an existing bundle, the operator MUST skip collection and redact the referenced bundle directly.
- **FR-004**: When obfuscation is enabled with both a source reference and an upload target, the operator MUST redact the referenced bundle and upload the cleaned result without re-collecting.
- **FR-005**: The default obfuscation policy MUST consistently replace IP addresses and MAC addresses across all files in the bundle and MUST omit Kubernetes Secret and ConfigMap content from the output.
- **FR-006**: The default obfuscation policy MUST preserve local loopback and unspecified local addresses without modification.
- **FR-007**: Administrators MUST be able to reference an optional custom obfuscation policy to override the default redaction rules.
- **FR-008**: Custom obfuscation policies MUST support configurable obfuscation targets (e.g., IP, MAC, pattern-based content) and configurable resource omission rules.
- **FR-009**: Every successfully obfuscated bundle MUST include an obfuscation audit log in the output.
- **FR-010**: When obfuscation uses a source reference, the original bundle on source storage MUST remain unmodified.
- **FR-011**: Obfuscation MUST be optional; requests that omit obfuscation configuration MUST behave identically to the current operator release.
- **FR-012**: The operator MUST reject MustGather requests where obfuscation is enabled but neither a source reference nor an upload target is specified.
- **FR-013**: The operator MUST reject MustGather requests where a source reference is set but obfuscation is not enabled.
- **FR-014**: When obfuscation processing fails, the MustGather request MUST report failure and MUST NOT upload a partial or unredacted bundle in place of the failed redaction.
- **FR-015**: Obfuscation MUST NOT require additional container images beyond those already used by the operator's collection and upload workflow.
- **FR-016**: Obfuscation MUST NOT modify the must-gather-clean library's public API; the operator consumes the library as a dependency.

### Key Entities

- **MustGather Request**: The administrator's declarative request to collect, optionally redact, and optionally upload diagnostic data. Carries obfuscation settings, optional source storage reference, and optional upload target.
- **Obfuscation Configuration**: Settings controlling whether redaction runs, optional reference to a custom policy, and optional source storage for existing bundles.
- **Custom Obfuscation Policy**: Administrator-provided rules defining which data types to anonymize, replacement behavior, and which Kubernetes resource types to omit from output.
- **Diagnostic Bundle**: The collected or referenced set of cluster diagnostic files subject to redaction and upload.
- **Obfuscation Audit Log**: A record included in the output bundle documenting redaction activity for compliance review.
- **Upload Target**: The destination (e.g., Red Hat support case via SFTP) where a cleaned bundle is delivered after successful processing.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A cluster administrator can enable obfuscation on a standard MustGather collection request and receive an uploaded bundle with no cleartext IP or MAC addresses within one operator processing cycle.
- **SC-002**: In a bundle processed with the default policy, zero files containing Kubernetes Secret or ConfigMap resource definitions appear in the uploaded output.
- **SC-003**: When the same sensitive value appears in multiple files within a bundle, 100% of occurrences map to the same anonymized token.
- **SC-004**: An administrator referencing a valid custom obfuscation policy sees upload output that differs measurably from the default policy (e.g., MAC addresses preserved when the custom policy omits MAC obfuscation).
- **SC-005**: An administrator can redact and upload an existing bundle from persistent storage without triggering a new collection, completing within the same order of magnitude as upload-only processing plus documented redaction time for a ~200 MB bundle.
- **SC-006**: Every successfully obfuscated bundle contains an obfuscation audit log accessible without cluster-administrator intervention beyond normal bundle access.
- **SC-007**: Existing MustGather requests created before obfuscation was available continue to succeed after operator upgrade with no configuration changes.
- **SC-008**: Invalid obfuscation configuration (missing policy, malformed policy, or disallowed field combinations) results in a failed MustGather request with an observable failure reason, not a silent fallback to unredacted upload.

## Assumptions

- **A-001**: Cluster administrators manage MustGather requests via the existing operator API; no new standalone CLI is required for this feature.
- **A-002**: The feature enters as Tech Preview; obfuscation progress is not reported as a distinct phase in MustGather status (deferred to a future enhancement).
- **A-003**: Hypershift, standalone, single-node, MicroShift, and OKE deployments have no unique obfuscation requirements beyond the standard operator workflow.
- **A-004**: Custom obfuscation policies are stored as standard Kubernetes configuration objects in the operator namespace and referenced by name from the MustGather request.
- **A-005**: For obfuscate-only requests (source without upload), the operator produces cleaned output accessible to the administrator via documented post-completion access procedures defined during planning; durable output persistence mechanics are specified in the implementation plan.
- **A-006**: MustGather requests with obfuscation enabled but neither source nor upload target are rejected at request validation time (FR-012).
- **A-007**: Custom obfuscation policy validation occurs at processing time for Tech Preview; admission-time webhook validation is out of scope unless added in a later release.
- **A-008**: When a referenced custom obfuscation policy is missing or invalid, the processing workload fails and the MustGather request status reflects the error; no admission webhook is required for Tech Preview.
- **A-009**: Obfuscation of bundles from non-standard collection images is best-effort; the operator does not guarantee compatibility validation for arbitrary collection images.
- **A-010**: The existing MustGather ServiceAccount and security context model is sufficient with documented extensions for reading custom policies and source storage; detailed permission requirements are defined during repo assessment and planning.
- **A-011**: The must-gather-clean library is vendored into the operator; no runtime version skew with an external obfuscation service exists.
- **A-012**: Domain-specific obfuscation patterns (e.g., internal hostnames) are not part of the default policy; administrators supply these via custom obfuscation policies.
- **A-013**: Performance targets follow documented benchmarks: ~50 seconds for a ~200 MB lightly sensitive bundle and proportionally longer for larger or denser bundles; exact SLA thresholds are validated during Tech Preview feedback.
- **A-014**: Replacement mapping reports (`report.yaml`) are included in the output bundle but not surfaced in MustGather CR status for Tech Preview.
