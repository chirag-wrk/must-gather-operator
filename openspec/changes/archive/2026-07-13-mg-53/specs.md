# Feature Specification: Extensible Must-Gather Upload Targets

**Feature Branch**: `mg-53`

**Created**: 2026-07-13

**Status**: Draft

**Input**: User description: "MG-53 — Customize SFTP hostname for must-gather uploads; EP operator-upload-targets — extensible uploadTarget configuration with SFTP support"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Configure SFTP Upload to a Custom Host (Priority: P1)

As a cluster administrator, I need to specify which SFTP server receives must-gather bundles so that I can upload to non-production environments (for example, staging) during CI and local testing instead of being limited to a single hardcoded production hostname.

**Why this priority**: This is the core Jira requirement (MG-53). Without a configurable hostname, administrators cannot validate upload flows against staging or test infrastructure.

**Independent Test**: Create a must-gather collection with upload enabled and a custom SFTP hostname; verify the bundle appears at the configured destination after job completion.

**Acceptance Scenarios**:

1. **Given** a must-gather collection is configured for SFTP upload with a custom hostname (e.g., a staging server), **When** the collection and upload complete successfully, **Then** the compressed bundle is delivered to that hostname.
2. **Given** a must-gather collection is configured for SFTP upload without specifying a hostname, **When** the collection and upload complete successfully, **Then** the bundle is delivered to the default Red Hat support SFTP hostname.
3. **Given** a must-gather collection is configured with an unreachable or invalid SFTP hostname, **When** the upload phase runs, **Then** the job reports failure with connectivity or authentication errors visible in job status and logs.

---

### User Story 2 - Structured Upload Destination Configuration (Priority: P1)

As a cluster administrator, I want upload settings grouped under a single, typed upload destination so that I configure one coherent block (destination type plus type-specific settings) rather than scattered top-level fields.

**Why this priority**: The enhancement replaces legacy flat upload fields with a discriminated structure. This is required to deliver the custom hostname capability cleanly and to support future destination types without further breaking changes.

**Independent Test**: Configure a must-gather collection using the new upload destination structure with type SFTP and required credentials; verify collection and upload succeed.

**Acceptance Scenarios**:

1. **Given** an administrator defines an upload destination of type SFTP with case identifier, credential reference, and optional hostname, **When** the resource is accepted, **Then** the operator schedules a collection job that includes an upload phase using those settings.
2. **Given** an administrator defines an upload destination of type SFTP but omits required SFTP-specific settings, **When** the resource is submitted, **Then** the system rejects the configuration with a clear validation error before any upload job runs.
3. **Given** an administrator defines an upload destination type that does not match its nested configuration (e.g., SFTP type without SFTP settings), **When** the resource is submitted, **Then** the system rejects the configuration at admission time.

---

### User Story 3 - Disable Upload While Still Collecting (Priority: P2)

As a cluster administrator, I want to run must-gather collection without uploading so that I can gather diagnostics locally or to cluster storage when upload is not needed.

**Why this priority**: Upload is optional; many collections only need local or PVC-retained artifacts. This preserves existing operational flexibility.

**Independent Test**: Create a must-gather collection with no upload destination configured; verify collection completes and no upload phase runs.

**Acceptance Scenarios**:

1. **Given** a must-gather collection with no upload destination configured, **When** the collection completes, **Then** no upload phase runs and the bundle remains at the configured retention location (PVC or ephemeral job storage).
2. **Given** a must-gather collection that previously had upload enabled, **When** the administrator removes the upload destination configuration, **Then** subsequent collections skip upload while still performing gather.

---

### User Story 4 - Internal User Upload Mode (Priority: P2)

As a Red Hat internal cluster administrator, I need to indicate that upload credentials belong to an internal user so that uploads follow the correct support workflow for internal accounts.

**Why this priority**: Supported in the enhancement proposal; affects upload behavior for a distinct user class.

**Independent Test**: Configure upload with the internal-user indicator enabled; verify upload completes successfully for internal credentials.

**Acceptance Scenarios**:

1. **Given** SFTP upload is configured with the internal-user indicator set to true, **When** the upload phase runs, **Then** the upload proceeds using internal-user handling as documented for the operator.
2. **Given** SFTP upload is configured without specifying the internal-user indicator, **When** the upload phase runs, **Then** the system applies the default internal-user behavior defined by the operator.

---

### User Story 5 - Auditable Upload Configuration (Priority: P3)

As an OpenShift security engineer, I want all upload-related settings defined in one well-scoped configuration block so that I can audit and enforce policies on data exfiltration paths.

**Why this priority**: Supports security governance; does not block core upload functionality.

**Independent Test**: Review a must-gather resource specification and confirm all upload parameters reside under the upload destination block with an explicit destination type.

**Acceptance Scenarios**:

1. **Given** a must-gather resource with upload enabled, **When** a security reviewer inspects the specification, **Then** all upload parameters (destination type, hostname, case identifier, credential reference, internal-user flag) are grouped under the upload destination configuration.

---

### Edge Cases

- **When** SFTP upload is configured with valid syntax but the referenced credential secret is missing or malformed, **then** the operator reports a validation or runtime failure with a clear error before or during upload, and the must-gather resource status reflects the failure.
- **When** cluster-wide proxy settings apply, **then** upload connectivity respects proxy configuration and failures due to proxy misconfiguration are reported in job logs.
- **When** the SFTP server is reachable but credentials are invalid, **then** the upload phase fails and the failure is visible in job logs and resource conditions.
- **When** an administrator upgrades the operator while existing resources still use the legacy flat upload field layout, **then** those resources fail validation or report a degraded condition with guidance to migrate to the new upload destination structure; the operator does not silently ignore legacy fields.
- **When** an administrator configures a destination type other than SFTP, **then** the system rejects the configuration at admission time until additional destination types are supported.
- **When** upload is disabled (no upload destination), **then** the gather phase still runs and artifacts are retained per existing retention behavior (PVC or ephemeral storage).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST allow cluster administrators to configure SFTP as an upload destination type for must-gather collections.
- **FR-002**: System MUST allow administrators to specify an optional SFTP server hostname; when omitted, the system MUST use the default Red Hat support SFTP hostname.
- **FR-003**: System MUST require, for SFTP upload destinations, a case identifier and a reference to credentials stored in a cluster secret.
- **FR-004**: System MUST reject upload destination configurations that specify SFTP type without the required SFTP-specific settings, or that include SFTP settings when the destination type is not SFTP.
- **FR-005**: System MUST support an optional internal-user indicator for SFTP uploads with a documented default when unset.
- **FR-006**: System MUST skip the upload phase entirely when no upload destination is configured, while still performing must-gather collection.
- **FR-007**: System MUST group all upload-related settings under a single upload destination configuration with an explicit destination type discriminator.
- **FR-008**: System MUST NOT support legacy top-level upload fields after this enhancement; administrators MUST migrate existing resources to the new upload destination structure before or during operator upgrade.
- **FR-009**: System MUST surface upload failures (connectivity, authentication, invalid configuration) in must-gather job logs and resource status conditions.
- **FR-010**: System MUST restrict supported upload destination types to SFTP in this release; other destination types (e.g., object storage, HTTP) are out of scope.

### Key Entities *(include if feature involves data)*

- **Must-Gather Collection**: A cluster administrator-initiated diagnostic collection with optional upload; has gather settings, optional upload destination, service account reference, and observed status (including completion and failure conditions).
- **Upload Destination**: A typed configuration block selecting how and where artifacts are uploaded; includes destination type and type-specific settings. Only one destination may be active per collection.
- **SFTP Upload Settings**: Type-specific settings including case identifier, credential secret reference, optional hostname override, and optional internal-user indicator.
- **Credential Secret**: A namespaced secret referenced by the collection containing SFTP authentication material; validated at runtime if not valid at admission.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An administrator can configure a must-gather collection with SFTP upload to a staging hostname and observe the bundle at that destination after successful job completion.
- **SC-002**: An administrator can configure a must-gather collection with SFTP upload and no hostname override, and the bundle is delivered to the default Red Hat support SFTP hostname.
- **SC-003**: Invalid upload destination configurations (missing required SFTP settings, type/settings mismatch) are rejected before an upload job is created.
- **SC-004**: A must-gather collection without an upload destination completes gather successfully with no upload phase executed.
- **SC-005**: Upload failures due to unreachable hosts or bad credentials produce observable failure status and retrievable error detail in job logs within one job lifecycle.
- **SC-006**: After operator upgrade, resources still using the legacy flat upload field layout are not silently accepted; administrators receive clear indication that migration to the upload destination structure is required.

## Assumptions

- **A-001**: Primary users are cluster administrators on OpenShift clusters where the must-gather operator is installed; Red Hat internal administrators are a subset using the internal-user upload mode.
- **A-002**: Only SFTP upload destinations are in scope for this release; S3, HTTP, and other destination types are explicitly deferred.
- **A-003**: MicroShift and environments without the operator lifecycle manager are out of scope.
- **A-004**: The operator behaves identically on standalone and HyperShift-hosted clusters for upload destination configuration; no topology-specific upload behavior is required.
- **A-005**: Legacy top-level upload fields are removed as a breaking change; there is no runtime backward-compatibility path that accepts deprecated fields. Administrators must migrate resources to the upload destination structure before upgrading.
- **A-006**: On upgrade, non-compliant resources (still using legacy layout) are rejected at admission or reported via degraded resource conditions rather than blocking the entire operator upgrade process cluster-wide.
- **A-007**: The default SFTP hostname when none is specified is the production Red Hat support SFTP endpoint (`sftp.access.redhat.com`); staging and other hostnames are opt-in via explicit configuration.
- **A-008**: Credential secrets are referenced in the same namespace as the must-gather collection, consistent with existing operator behavior.
- **A-009**: Must-gather collection spec immutability after creation remains unchanged; administrators must delete and recreate a collection to change upload settings.
- **A-010**: Existing proxy, timeout, audit, and service-account configuration on must-gather collections continue to work independently of upload destination settings.
