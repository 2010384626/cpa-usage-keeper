package api

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

type usageTransferStub struct {
	usageEventsStub

	exportPayload *service.UsageExport
	importPayload service.UsageExport
	importResult  service.UsageImportResult
	err           error
}

func (s *usageTransferStub) ExportUsage(context.Context) (*service.UsageExport, error) {
	return s.exportPayload, s.err
}

func (s *usageTransferStub) ImportUsage(_ context.Context, payload service.UsageExport) (service.UsageImportResult, error) {
	s.importPayload = payload
	return s.importResult, s.err
}

func TestUsageExportReturnsAttachmentJSON(t *testing.T) {
	provider := &usageTransferStub{
		exportPayload: &service.UsageExport{
			SchemaVersion: service.UsageExportSchemaVersion,
			ExportedAt:    time.Date(2026, 5, 6, 8, 0, 0, 0, time.UTC),
			Source:        "cpa-usage-keeper",
			Events: []service.UsageExportEvent{{
				EventKey:  "event-1",
				Model:     "gpt-5",
				Timestamp: time.Date(2026, 5, 6, 7, 0, 0, 0, time.UTC),
				Tokens:    service.UsageExportTokenStats{TotalTokens: 42},
			}},
		},
	}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/export", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if disposition := resp.Header().Get("Content-Disposition"); !contains(disposition, `attachment; filename="cpa-usage-export-`) {
		t.Fatalf("expected attachment content disposition, got %q", disposition)
	}
	body := resp.Body.String()
	if !contains(body, `"schema_version":1`) || !contains(body, `"event_key":"event-1"`) || !contains(body, `"total_tokens":42`) {
		t.Fatalf("unexpected export body: %s", body)
	}
}

func TestUsageImportAcceptsMultipartJSON(t *testing.T) {
	provider := &usageTransferStub{
		importResult: service.UsageImportResult{
			TotalEvents:    2,
			InsertedEvents: 1,
			SkippedEvents:  1,
		},
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "usage.json")
	if err != nil {
		t.Fatalf("CreateFormFile returned error: %v", err)
	}
	_, _ = part.Write([]byte(`{"schema_version":1,"events":[{"event_key":"event-1","model":"gpt-5","timestamp":"2026-05-06T07:00:00Z","tokens":{"total_tokens":42}}]}`))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/usage/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(provider.importPayload.Events) != 1 || provider.importPayload.Events[0].EventKey != "event-1" {
		t.Fatalf("expected import payload to be decoded, got %+v", provider.importPayload)
	}
	if body := resp.Body.String(); !contains(body, `"total_events":2`) || !contains(body, `"inserted_events":1`) || !contains(body, `"skipped_events":1`) {
		t.Fatalf("unexpected import response body: %s", body)
	}
}

func TestUsageImportRejectsInvalidJSON(t *testing.T) {
	router := NewRouter(nil, nil, &usageTransferStub{}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/usage/import", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
	if !contains(resp.Body.String(), `"error":"invalid import file"`) {
		t.Fatalf("unexpected response body: %s", resp.Body.String())
	}
}

func TestUsageImportRejectsUnsupportedSchemaVersion(t *testing.T) {
	router := NewRouter(nil, nil, &usageTransferStub{}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/usage/import", bytes.NewBufferString(`{"schema_version":2,"events":[]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
	if !contains(resp.Body.String(), `"error":"unsupported usage export schema version"`) {
		t.Fatalf("unexpected response body: %s", resp.Body.String())
	}
}
