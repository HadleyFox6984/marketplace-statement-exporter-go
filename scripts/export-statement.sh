#!/bin/sh
set -eu

curl --fail-with-body \
  --request POST \
  --header 'Content-Type: application/json' \
  --data '{
    "statement_id": "stmt-2026-09-acme",
    "seller": "Acme Data Cooperative",
    "buyer": "Harbor Analytics",
    "order_id": "order-1842",
    "period": "2026-09",
    "assets": [
      {"name": "orders.parquet", "kind": "dataset"},
      {"name": "schema.json", "kind": "schema"}
    ],
    "updates": [
      {"at": "2026-09-18", "message": "Buyer validation completed", "visibility": "buyer"},
      {"at": "2026-09-19", "message": "Monthly partition compacted", "visibility": "seller"}
    ],
    "handoff": {
      "status": "complete",
      "completed_at": "2026-09-20T14:30:00Z",
      "reference": "delivery-884"
    }
  }' \
  http://localhost:8080/statements
