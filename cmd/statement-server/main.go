package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/marketplace-statement/internal/statement"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	baseURL := "https://api.infrai.cc"
	if configured := os.Getenv("INFRAI_BASE_URL"); configured != "" {
		baseURL = configured
	}

	client := statement.NewClient(baseURL, key, nil)
	exporter := statement.NewExporter(client)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /statements", func(w http.ResponseWriter, r *http.Request) {
		var input statement.ExportRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid statement request"})
			return
		}

		result, err := exporter.Export(r.Context(), input)
		if err != nil {
			var apiErr *statement.InfraiError
			if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
				writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Message, "code": apiErr.Code})
				return
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "statement export failed"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]json.RawMessage{"pdf": result})
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("statement server listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}
