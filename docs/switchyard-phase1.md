# Switchyard scaffolding — Lyceum (LYCM)

Reproducible payloads for the project as provisioned. The live project, 4 epics,
23 tickets, and dependency links are already created via the Switchyard MCP.

## Project

```json
{
  "key": "LYCM",
  "name": "Lyceum",
  "color": "#7c3aed",
  "repo_url": "file:///home/magos/projects/lyceum",
  "default_test_cmd": "go test ./..."
}
```

> Switchyard assigns sequential ticket keys (`LYCM-1`, `LYCM-2`, …). The
> phase-style identifiers (`LYCM-100`, `LYCM-101`, …) from the brief are carried
> in **ticket titles** as stable human labels. Mapping below.

## Epics → real keys

| Label    | Real key | Title                                      |
|----------|----------|--------------------------------------------|
| LYCM-100 | LYCM-1   | Core Foundation (Go & Storage)             |
| LYCM-200 | LYCM-2   | Web Reader (TypeScript & epub.js)          |
| LYCM-300 | LYCM-3   | Cross-Platform Wrappers                    |
| LYCM-400 | LYCM-4   | Ecosystem & Agent Integration             |

## Phase 1 tickets → real keys

| Label    | Real key | Title                                   | Concurrency        |
|----------|----------|-----------------------------------------|--------------------|
| LYCM-101 | LYCM-5   | Bootstrap Go module & layout            | serial (first)     |
| LYCM-102 | LYCM-6   | Postgres schema & migrations            | serial (after 101) |
| LYCM-103 | LYCM-7   | Storage/repository layer                | after 102          |
| LYCM-104 | LYCM-8   | EPUB metadata parser                    | ‖ after 101        |
| LYCM-105 | LYCM-9   | POST /upload                            | after 103 + 104    |
| LYCM-106 | LYCM-10  | GET /library (+ covers)                 | after 103          |
| LYCM-107 | LYCM-11  | /sync (CFI position)                    | after 103 + 108    |
| LYCM-108 | LYCM-12  | EPUB CFI parse/validate util            | ‖ after 101        |

Each ticket carries `metadata.test_cmd` so the cogitation engine can validate it
independently. Per-ticket commands override the project `default_test_cmd`.

> Storage engine: the shared construct-server Postgres 16 (`postgres` container,
> `127.0.0.1:5432`). The `lyceum` / `lyceum_test` databases and `lyceum_user`
> role are provisioned by `~/construct-server/db/init-db.sh`. Tests connect via
> `TEST_DATABASE_URL` (the `lyceum_test` database); see the repo `.env`.

## Example: re-create a ticket (MCP payload)

```json
{
  "project_key": "LYCM",
  "type": "task",
  "parent_id": "<LYCM-1 epic UUID>",
  "title": "LYCM-104 — EPUB metadata parser (title, author, cover)",
  "priority": "high",
  "metadata": {
    "component": "backend",
    "mode": "create",
    "test_cmd": "go test ./internal/epub/...",
    "concurrency": "parallel-after-101"
  }
}
```

## Suggested concurrent agentic kickoff (Phase 1)

1. **Serial:** LYCM-101 → LYCM-102.
2. **Fan out (3 agents in parallel)** once 101/102 land:
   - LYCM-103 (storage)
   - LYCM-104 (EPUB parser) — pure, isolated
   - LYCM-108 (CFI util) — pure, isolated
3. **Fan out (3 agents)** once 103 (+104/108) land:
   - LYCM-105 (/upload), LYCM-106 (/library), LYCM-107 (/sync)

Worktree isolation recommended for the step-2/step-3 fan-outs since they touch
overlapping packages.
