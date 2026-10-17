package statement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	usagePath    = "/v1/account/usage/timeseries"
	generatePath = "/v1/pdf/generate"
)

type Client struct {
	baseURL    string
	key        string
	http       *http.Client
	maxRetries int
	sleep      func(context.Context, time.Duration) error
}

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewClient(baseURL, key string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		key:        key,
		http:       httpClient,
		maxRetries: 3,
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (c *Client) UsageTimeseries(ctx context.Context) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, usagePath, nil, "")
}

func (c *Client) GeneratePDF(ctx context.Context, statementID, markdown string) (json.RawMessage, error) {
	body := struct {
		Markdown    string `json:"markdown"`
		PageSize    string `json:"page_size"`
		Orientation string `json:"orientation"`
		Store       bool   `json:"store"`
	}{
		Markdown:    markdown,
		PageSize:    "A4",
		Orientation: "portrait",
		Store:       true,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode PDF request: %w", err)
	}
	return c.do(ctx, http.MethodPost, generatePath, encoded, statementID)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, idempotencyKey string) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build Infrai request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("call Infrai: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.maxRetries {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return nil, err
			}
			continue
		}
		if !env.OK {
			apiErr := &InfraiError{HTTPStatus: res.StatusCode, Message: "request rejected"}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
			}
			return nil, apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return nil, fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		return env.Data, nil
	}
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Second << attempt
}

type Asset struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type Update struct {
	At         string `json:"at"`
	Message    string `json:"message"`
	Visibility string `json:"visibility"`
}

type Handoff struct {
	Status      string `json:"status"`
	CompletedAt string `json:"completed_at"`
	Reference   string `json:"reference"`
}

type ExportRequest struct {
	StatementID string   `json:"statement_id"`
	Seller      string   `json:"seller"`
	Buyer       string   `json:"buyer"`
	OrderID     string   `json:"order_id"`
	Period      string   `json:"period"`
	Assets      []Asset  `json:"assets"`
	Updates     []Update `json:"updates"`
	Handoff     Handoff  `json:"handoff"`
}

type Exporter struct {
	client *Client
}

func NewExporter(client *Client) *Exporter {
	return &Exporter{client: client}
}

func (e *Exporter) Export(ctx context.Context, input ExportRequest) (json.RawMessage, error) {
	if input.StatementID == "" || input.Seller == "" || input.Buyer == "" || input.OrderID == "" {
		return nil, errors.New("statement_id, seller, buyer, and order_id are required")
	}
	usage, err := e.client.UsageTimeseries(ctx)
	if err != nil {
		return nil, err
	}
	markdown, err := renderStatement(input, usage)
	if err != nil {
		return nil, err
	}
	return e.client.GeneratePDF(ctx, input.StatementID, markdown)
}

func renderStatement(input ExportRequest, usage json.RawMessage) (string, error) {
	var prettyUsage bytes.Buffer
	if err := json.Indent(&prettyUsage, usage, "", "  "); err != nil {
		return "", fmt.Errorf("format metered usage: %w", err)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "# Marketplace account statement\n\n")
	fmt.Fprintf(&out, "Statement: %s  \nPeriod: %s  \nSeller: %s  \nBuyer: %s  \nOrder: %s\n\n", input.StatementID, input.Period, input.Seller, input.Buyer, input.OrderID)
	fmt.Fprintf(&out, "## Seller assets\n\n| Asset | Type |\n|---|---|\n")
	for _, asset := range input.Assets {
		fmt.Fprintf(&out, "| %s | %s |\n", asset.Name, asset.Kind)
	}
	fmt.Fprintf(&out, "\n## Buyer updates\n\n")
	visible := 0
	for _, update := range input.Updates {
		if update.Visibility == "buyer" {
			fmt.Fprintf(&out, "- %s: %s\n", update.At, update.Message)
			visible++
		}
	}
	if visible == 0 {
		fmt.Fprintf(&out, "No buyer-visible updates in this period.\n")
	}
	fmt.Fprintf(&out, "\n## Order handoff\n\nStatus: %s  \nCompleted: %s  \nReference: %s\n", input.Handoff.Status, input.Handoff.CompletedAt, input.Handoff.Reference)
	fmt.Fprintf(&out, "\n## Metered usage\n\n```json\n%s\n```\n", prettyUsage.String())
	return out.String(), nil
}
