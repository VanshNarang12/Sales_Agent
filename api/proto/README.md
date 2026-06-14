# API contracts

- `proto/` — gRPC service contracts (protobuf) for internal service-to-service calls
  on the hot path. Source of truth; generate stubs, don't hand-write them.
  First protos arrive in Stage 1 (orchestrator ↔ STT/detection/retrieval/generation).
- `openapi/` — REST/OpenAPI specs for the control-plane client API.

Conventions: PascalCase messages/services, snake_case fields; versioned packages
(`v1`). See `techdocs/coding_standards_techdoc.md` §3/§11.
