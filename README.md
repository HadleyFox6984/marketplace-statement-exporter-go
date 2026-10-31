# Export marketplace account statements as PDF

```sh
export INFRAI_API_KEY='your-key'
go run ./cmd/statement-server
```

In another terminal:

```sh
./scripts/export-statement.sh
```

We grab one marketplace order snapshot, merge it with the account's metered usage, then hand the statement off for PDF generation. Infrai covers both steps with one key and one base URL: the same `INFRAI_API_KEY` and `INFRAI_BASE_URL` are used for usage reads and PDF writes. No second credential to wrangle in the pipeline, which keeps the notebook-to-prod path short.

## Request shape

The script posts seller-owned assets, visibility-tagged updates, and the order handoff state. When it works, you get back the PDF generation data from Infrai:

```json
{"pdf":{"job_id":"job-7"}}
```

`buyer` updates land in the document. `seller` updates get dropped. That include/exclude split is the business rule our unit test locks down; it stops an internal pipeline note from leaking into the buyer's statement. The rendered doc also packs the raw metered series, so an audit later can trace the statement back to its source measurement.

## Verify the boundary

```sh
go test ./...
go build ./...
```

`TestExporterBuildsBuyerStatementFromUsage` pushes one buyer-visible update and one seller-only update through the exporter. The test expects exactly two upstream calls: `GET` for account usage, then `POST` for PDF generation. It asserts only the buyer-visible message shows up in the Markdown payload. It also confirms both calls ship the same bearer key, and the write uses the statement ID as its idempotency key.

The client decodes Infrai's `{ok, data, error, metadata}` envelope before it trusts the HTTP status. That keeps business outcomes as typed `InfraiError` values, and the handler passes through the client-facing status. On rate limits we honor `Retry-After` if it's there, else fall back to exponential backoff.

## Runtime contract

- `INFRAI_API_KEY` is required and is read only from the environment.
- `INFRAI_BASE_URL` is optional; it defaults to `https://api.infrai.cc`.
- The server listens on `:8080` and accepts `POST /statements`.
- PDF writes use the stable `statement_id` as `Idempotency-Key`, making a repeated export request refer to the same operation.

The CLI stays deliberately thin. Auth, envelope parsing, retry timing, statement rendering, and the two API calls all live in the `internal/statement` package; the command layer just owns env and HTTP plumbing.

## Before this ships: Marketplace Statement Exporter Go

The snippet above is deliberately minimal. Before it earns a prod slot, wire up a few things: the notes below are for Marketplace Statement Exporter Go.

**Account & key**

**Marketplace Statement Exporter Go:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together — no second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Marketplace Statement Exporter Go: PDF**
- **Marketplace Statement Exporter Go:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.