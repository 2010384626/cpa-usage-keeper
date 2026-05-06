package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageTransferExportAndImportRoundTrip(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-transfer.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	existing := models.UsageEvent{
		EventKey:        "event-existing",
		APIGroupKey:     "provider-a",
		Provider:        "Provider A",
		Endpoint:        "/v1/chat/completions",
		AuthType:        "apikey",
		RequestID:       "request-existing",
		Model:           "gpt-5",
		Timestamp:       time.Date(2026, 5, 5, 11, 0, 0, 0, time.UTC),
		Source:          "source-a",
		AuthIndex:       "auth-a",
		Failed:          false,
		LatencyMS:       123,
		InputTokens:     10,
		OutputTokens:    4,
		ReasoningTokens: 2,
		CachedTokens:    1,
		TotalTokens:     16,
	}
	later := models.UsageEvent{
		EventKey:    "event-later",
		APIGroupKey: "provider-b",
		Model:       "claude-sonnet",
		Timestamp:   time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		TotalTokens: 20,
	}
	if _, _, err := repository.InsertUsageEvents(db, []models.UsageEvent{later, existing}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	transfer, ok := NewUsageService(db).(UsageTransferProvider)
	if !ok {
		t.Fatal("NewUsageService should implement UsageTransferProvider")
	}

	exported, err := transfer.ExportUsage(context.Background())
	if err != nil {
		t.Fatalf("ExportUsage returned error: %v", err)
	}
	if exported.SchemaVersion != UsageExportSchemaVersion || exported.Source != usageExportSource {
		t.Fatalf("unexpected export metadata: %+v", exported)
	}
	if len(exported.Events) != 2 || exported.Events[0].EventKey != "event-existing" || exported.Events[1].EventKey != "event-later" {
		t.Fatalf("expected timestamp-ordered events, got %+v", exported.Events)
	}
	if exported.Events[0].RequestID != "request-existing" || exported.Events[0].Tokens.TotalTokens != 16 || exported.Events[0].Endpoint != "/v1/chat/completions" {
		t.Fatalf("expected full event fields to be exported, got %+v", exported.Events[0])
	}

	result, err := transfer.ImportUsage(context.Background(), UsageExport{
		SchemaVersion: UsageExportSchemaVersion,
		Events: []UsageExportEvent{
			exported.Events[0],
			{
				EventKey:    "event-new",
				APIGroupKey: "provider-c",
				Model:       "gpt-5.1",
				Timestamp:   time.Date(2026, 5, 5, 13, 0, 0, 0, time.UTC),
				Tokens:      UsageExportTokenStats{InputTokens: 30, OutputTokens: 7, TotalTokens: 37},
			},
			{EventKey: "", Timestamp: time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC)},
		},
	})
	if err != nil {
		t.Fatalf("ImportUsage returned error: %v", err)
	}
	if result.TotalEvents != 3 || result.InsertedEvents != 1 || result.SkippedEvents != 1 || result.FailedEvents != 1 {
		t.Fatalf("unexpected import result: %+v", result)
	}

	var count int64
	if err := db.Model(&models.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected three stored events after import, got %d", count)
	}
}

func TestUsageImportRejectsUnsupportedSchemaVersion(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-transfer-schema.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	transfer := NewUsageService(db).(UsageTransferProvider)
	result, err := transfer.ImportUsage(context.Background(), UsageExport{
		SchemaVersion: UsageExportSchemaVersion + 1,
		Events: []UsageExportEvent{{
			EventKey:  "event-1",
			Timestamp: time.Date(2026, 5, 5, 11, 0, 0, 0, time.UTC),
		}},
	})
	if err == nil {
		t.Fatal("expected unsupported schema version error")
	}
	if result.TotalEvents != 1 || result.InsertedEvents != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
}
