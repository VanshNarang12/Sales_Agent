# Deploy

Infrastructure-as-code for the Sales Copilot.

- `terraform/` — cloud infra (AWS): VPC, EKS, RDS Postgres (+pgvector), ElastiCache
  Redis, S3, event bus, secrets/KMS. **Skeleton only in Stage 0**; hardened through
  Stages 22/24 (`ROADMAP.md`).
- `k8s/` — Kubernetes manifests/Helm. Hot-path services run in a dedicated,
  compute-optimized node pool in one AZ for predictable latency; async/batch on a
  separate pool (incl. spot). See `ARCHITECTURE.md` §17.

Local development does not need this — use `docker-compose.yml` at the repo root
(`make up`).
