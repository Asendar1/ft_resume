# applications-api

A small REST API in Go for tracking job applications. Built to practise the backend
fundamentals: validated endpoints, integrity enforced by the database, one LLM-backed
feature, and table-driven tests.

## Stack

- Go standard library `net/http` (method + wildcard routing, Go 1.22+)
- `database/sql` over SQLite (`modernc.org/sqlite`, pure Go, no cgo)
- Plain `http.Client` against any OpenAI-compatible chat-completions endpoint

## Endpoints

| Method | Path | Notes |
|---|---|---|
| `POST` | `/applications` | Validates the body; `UNIQUE(company, role)` → `409` on duplicate |
| `GET` | `/applications?status=&limit=` | Filter + paginate; `400` on a bad filter or limit |
| `PATCH` | `/applications/{id}/status` | `404` if unknown, `422` if the status is not in the enum |
| `POST` | `/applications/{id}/advice` | Calls the model with a 12s ceiling; `503` if unconfigured, `502` if the call fails |

Statuses: `applied`, `screen`, `interview`, `offer`, `rejected`.

## Where the guarantees live

- **Schema, not code:** `schema.sql` carries the length bounds, the status `CHECK`,
  and `UNIQUE(company, role)` with `COLLATE NOCASE`. The handler only translates a
  constraint violation into `409`.
- **Validation at the boundary:** bad input is rejected with `422` and a per-field
  error map before any query runs.
- **Upstream failures are not our failures:** a model error becomes `502`, and the
  upstream response body is never echoed back to the client.

## Run

```sh
go run .                              # :8080, ./applications.db
ADDR=:9000 DB_PATH=data/app.db go run .

# model feature (any OpenAI-compatible endpoint)
LLM_API_KEY=... LLM_BASE_URL=https://openrouter.ai/api/v1 LLM_MODEL=... go run .
```

## Test

```sh
go test ./...
```

Covers: validation table, duplicate rejection through the schema constraint,
filter/limit handling, status update `404`/`422`, and the advice endpoint's
`200`/`502`/`503` paths against a fake model client.
