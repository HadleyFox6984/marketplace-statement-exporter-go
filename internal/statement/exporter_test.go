package statement

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExporterBuildsBuyerStatementFromUsage(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case usagePath:
			if r.Method != http.MethodGet {
				t.Fatalf("usage method = %s", r.Method)
			}
			io.WriteString(w, `{"ok":true,"data":{"series":[{"meter":"pdf","quantity":7}]},"metadata":{}}`)
		case generatePath:
			if r.Method != http.MethodPost {
				t.Fatalf("generate method = %s", r.Method)
			}
			if got := r.Header.Get("Idempotency-Key"); got != "stmt-42" {
				t.Fatalf("idempotency key = %q", got)
			}
			var body struct {
				Markdown string `json:"markdown"`
				Store    bool   `json:"store"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			for _, wanted := range []string{"asset.csv", "Buyer approved delivery", `"quantity": 7`, "Status: complete"} {
				if !strings.Contains(body.Markdown, wanted) {
					t.Errorf("markdown missing %q\n%s", wanted, body.Markdown)
				}
			}
			if strings.Contains(body.Markdown, "internal reconciliation") {
				t.Error("seller-only update was exposed to buyer")
			}
			if !body.Store {
				t.Error("store must be true")
			}
			io.WriteString(w, `{"ok":true,"data":{"job_id":"job-7"},"metadata":{}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exporter := NewExporter(NewClient(server.URL, "test-key", server.Client()))
	result, err := exporter.Export(context.Background(), ExportRequest{
		StatementID: "stmt-42",
		Seller:      "Northwind Data",
		Buyer:       "Contoso Retail",
		OrderID:     "order-19",
		Period:      "2026-09",
		Assets:      []Asset{{Name: "asset.csv", Kind: "dataset"}},
		Updates: []Update{
			{At: "2026-09-18", Message: "Buyer approved delivery", Visibility: "buyer"},
			{At: "2026-09-19", Message: "internal reconciliation", Visibility: "seller"},
		},
		Handoff: Handoff{Status: "complete", CompletedAt: "2026-09-20", Reference: "delivery-8"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"job_id":"job-7"}` {
		t.Fatalf("result = %s", result)
	}
	if got := strings.Join(calls, ", "); got != "GET "+usagePath+", POST "+generatePath {
		t.Fatalf("calls = %s", got)
	}
}

func TestClientRetriesRateLimitAfterEnvelopeDecode(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"ok":false,"error":{"message":"retry later"},"metadata":{}}`)
			return
		}
		io.WriteString(w, `{"ok":true,"data":{"series":[]},"metadata":{}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", server.Client())
	client.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := client.UsageTimeseries(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}
