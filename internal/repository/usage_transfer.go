package repository

import (
	"fmt"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func ListUsageEventsForExport(db *gorm.DB) ([]models.UsageEvent, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var events []models.UsageEvent
	if err := db.Model(&models.UsageEvent{}).Order("timestamp ASC, id ASC").Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events for export: %w", err)
	}
	for i := range events {
		events[i].Timestamp = events[i].Timestamp.UTC()
	}
	return events, nil
}
