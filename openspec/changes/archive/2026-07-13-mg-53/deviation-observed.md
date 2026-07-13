# Deviations Observed

**Change**: mg-53  
**Jira**: MG-53

## T7_2 — Final preflight skipped

**Task ID**: T7_2  
**Rationale**: User requested skipping `make validate`, `make lint`, `make go-test`, and controller test preflight. Prior partial attempts failed on dirty-repo boilerplate check and missing local `golangci-lint`.

**Impact**: Full repo preflight not verified locally. CI should run standard Makefile targets before merge.

## T2_1 — OpenAPI generation workaround (from T2_1 report)

**Task ID**: T2_1  
**Rationale**: Full `make generate` failed locally on `openapi-generate` flag mismatch; used `make op-generate` for CRD/deepcopy only.

**Impact**: OpenAPI artifacts may need regeneration in CI if required by pipeline.
