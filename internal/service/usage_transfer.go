package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

const (
	UsageExportSchemaVersion = 1
	usageExportSource        = "cpa-usage-keeper"
)

type UsageTransferProvider interface {
	ExportUsage(context.Context) (*UsageExport, error)
	ImportUsage(context.Context, UsageExport) (UsageImportResult, error)
}

type UsageExport struct {
	SchemaVersion int                `json:"schema_version"`
	ExportedAt    time.Time          `json:"exported_at"`
	Source        string             `json:"source"`
	Events        []UsageExportEvent `json:"events"`
}

type UsageExportEvent struct {
	EventKey    string                `json:"event_key"`
	APIGroupKey string                `json:"api_group_key"`
	Provider    string                `json:"provider,omitempty"`
	Endpoint    string                `json:"endpoint,omitempty"`
	AuthType    string                `json:"auth_type,omitempty"`
	RequestID   string                `json:"request_id,omitempty"`
	Model       string                `json:"model"`
	Timestamp   time.Time             `json:"timestamp"`
	Source      string                `json:"source,omitempty"`
	AuthIndex   string                `json:"auth_index,omitempty"`
	Failed      bool                  `json:"failed"`
	LatencyMS   int64                 `json:"latency_ms"`
	Tokens      UsageExportTokenStats `json:"tokens"`
}

type UsageExportTokenStats struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type UsageImportResult struct {
	TotalEvents    int `json:"total_events"`
	InsertedEvents int `json:"inserted_events"`
	SkippedEvents  int `json:"skipped_events"`
	FailedEvents   int `json:"failed_events"`
}

func (s *usageService) ExportUsage(ctx context.Context) (*UsageExport, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	db := withUsageTransferContext(s.db, ctx)
	rows, err := repository.ListUsageEventsForExport(db)
	if err != nil {
		return nil, err
	}

	events := make([]UsageExportEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, usageExportEventFromModel(row))
	}

	return &UsageExport{
		SchemaVersion: UsageExportSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		Source:        usageExportSource,
		Events:        events,
	}, nil
}

func (s *usageService) ImportUsage(ctx context.Context, payload UsageExport) (UsageImportResult, error) {
	result := UsageImportResult{TotalEvents: len(payload.Events)}
	if s == nil || s.db == nil {
		return result, fmt.Errorf("database is nil")
	}
	if payload.SchemaVersion != 0 && payload.SchemaVersion != UsageExportSchemaVersion {
		return result, fmt.Errorf("unsupported usage export schema version %d", payload.SchemaVersion)
	}

	events := make([]models.UsageEvent, 0, len(payload.Events))
	for _, item := range payload.Events {
		event, ok := usageExportEventToModel(item)
		if !ok {
			result.FailedEvents++
			continue
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return result, nil
	}

	db := withUsageTransferContext(s.db, ctx)
	aligned, err := alignUsageEventKeysWithExistingCanonicalEvents(db, events)
	if err != nil {
		return result, fmt.Errorf("align usage events: %w", err)
	}
	inserted, skipped, err := repository.InsertUsageEvents(db, aligned)
	if err != nil {
		return result, err
	}
	result.InsertedEvents = inserted
	result.SkippedEvents = skipped
	return result, nil
}

func withUsageTransferContext(db *gorm.DB, ctx context.Context) *gorm.DB {
	if db == nil || ctx == nil {
		return db
	}
	return db.WithContext(ctx)
}

func usageExportEventFromModel(row models.UsageEvent) UsageExportEvent {
	return UsageExportEvent{
		EventKey:    strings.TrimSpace(row.EventKey),
		APIGroupKey: strings.TrimSpace(row.APIGroupKey),
		Provider:    strings.TrimSpace(row.Provider),
		Endpoint:    strings.TrimSpace(row.Endpoint),
		AuthType:    strings.TrimSpace(row.AuthType),
		RequestID:   strings.TrimSpace(row.RequestID),
		Model:       strings.TrimSpace(row.Model),
		Timestamp:   row.Timestamp.UTC(),
		Source:      strings.TrimSpace(row.Source),
		AuthIndex:   strings.TrimSpace(row.AuthIndex),
		Failed:      row.Failed,
		LatencyMS:   row.LatencyMS,
		Tokens: UsageExportTokenStats{
			InputTokens:     row.InputTokens,
			OutputTokens:    row.OutputTokens,
			ReasoningTokens: row.ReasoningTokens,
			CachedTokens:    row.CachedTokens,
			TotalTokens:     row.TotalTokens,
		},
	}
}

func usageExportEventToModel(item UsageExportEvent) (models.UsageEvent, bool) {
	eventKey := strings.TrimSpace(item.EventKey)
	if eventKey == "" || item.Timestamp.IsZero() {
		return models.UsageEvent{}, false
	}
	return models.UsageEvent{
		EventKey:        eventKey,
		APIGroupKey:     strings.TrimSpace(item.APIGroupKey),
		Provider:        strings.TrimSpace(item.Provider),
		Endpoint:        strings.TrimSpace(item.Endpoint),
		AuthType:        strings.TrimSpace(item.AuthType),
		RequestID:       strings.TrimSpace(item.RequestID),
		Model:           strings.TrimSpace(item.Model),
		Timestamp:       item.Timestamp.UTC(),
		Source:          strings.TrimSpace(item.Source),
		AuthIndex:       strings.TrimSpace(item.AuthIndex),
		Failed:          item.Failed,
		LatencyMS:       item.LatencyMS,
		InputTokens:     item.Tokens.InputTokens,
		OutputTokens:    item.Tokens.OutputTokens,
		ReasoningTokens: item.Tokens.ReasoningTokens,
		CachedTokens:    item.Tokens.CachedTokens,
		TotalTokens:     item.Tokens.TotalTokens,
	}, true
}
