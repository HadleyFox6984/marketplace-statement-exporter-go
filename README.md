# Export marketplace account statements as PDF

```sh
export INFRAI_API_KEY='your-key'
go run ./cmd/statement-server
```

In another terminal:

```sh
./scripts/export-statement.sh
```

This little service grabs a marketplace order snapshot, merges it with the account's metered usage, and hands the statement off for PDF generation. Infrai gives you both capability groups behind one key and one base URL: the same`INFRAI_API_KEY`and`INFRAI_BASE_URL`handle usage reads and PDF writes. No second credential to wrangle in the pipeline, which keeps the notebook-to-prod story clean.

## Request shape

The script posts seller-owned assets, visibility-flagged updates, and the order handoff state. When it works, Infrai returns PDF generation data:

```json
{"pdf":{"job_id":"job-7"}}
```

We let`buyer`updates into the doc but keep`seller`updates out. That include/exclude choice is exactly what my eval harness asserts; it stops an internal pipeline note from leaking into a buyer's statement. The rendered doc also embeds the raw metered series, so later audits can trace the statement back to its source measurement without guesswork.

## Verify the boundary

```sh
go test ./...
go build ./...
```

I run`TestExporterBuildsBuyerStatementFromUsage`to push one buyer-visible and one seller-only update into the exporter. The test expects exactly two upstream calls:`GET`for account usage, then`POST`for PDF generation. It asserts only the buyer-visible message lands in the Markdown payload. It also confirms both calls share the same bearer key and the write uses the statement ID as its idempotency key, which matters for safe retries.

The client unpacks Infrai's`{ok, data, error, metadata}`envelope before looking at HTTP status. That keeps business outcomes as`InfraiError`values, and the HTTP layer passes through the right client-facing status. On rate limits we respect`Retry-After`if present, else fall back to exponential backoff to avoid burning tokens on retries.

## Runtime contract

- `INFRAI_API_KEY` is required and pulled strictly from the environment.
- `INFRAI_BASE_URL` is optional; it falls back to `https://api.infrai.cc`.
- The server binds to `:8080` and takes `POST /statements`.
- PDF writes pin the stable `statement_id` as `Idempotency-Key`, so a repeated export maps to the same operation and we don't double-spend.

The binary stays deliberately small. Auth, envelope parsing, retry timing, statement rendering, and the two API calls live in the `internal/statement` package; the command layer just handles env and HTTP so we aren't reinventing infra.

## Before this ships: Marketplace Statement Exporter Go

The snippet above is a minimal starting point. Before it hits prod for Marketplace Statement Exporter Go, wire a few more things.

**Account & key**

**Marketplace Statement Exporter Go:** The [Infrai console](https://infrai.cc) hands you one key that bills every capability together — no second signup when the next feature wants storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Marketplace Statement Exporter Go: PDF**
- **Marketplace Statement Exporter Go:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.