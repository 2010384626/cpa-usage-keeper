package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

const usageImportMaxBytes int64 = 100 << 20

type usageTransferProvider interface {
	ExportUsage(context.Context) (*service.UsageExport, error)
	ImportUsage(context.Context, service.UsageExport) (service.UsageImportResult, error)
}

func registerUsageTransferRoutes(router gin.IRoutes, usageProvider service.UsageProvider) {
	router.GET("/usage/export", func(c *gin.Context) {
		provider, ok := usageProvider.(usageTransferProvider)
		if !ok || provider == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "usage transfer is not configured"})
			return
		}

		payload, err := provider.ExportUsage(c.Request.Context())
		if err != nil {
			writeInternalError(c, "export usage failed", err)
			return
		}
		if payload.Events == nil {
			payload.Events = []service.UsageExportEvent{}
		}

		filename := fmt.Sprintf("cpa-usage-export-%s.json", time.Now().UTC().Format("20060102T150405Z"))
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, payload)
	})

	router.POST("/usage/import", func(c *gin.Context) {
		provider, ok := usageProvider.(usageTransferProvider)
		if !ok || provider == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "usage transfer is not configured"})
			return
		}

		payload, err := readUsageImportPayload(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid import file"})
			return
		}
		if payload.SchemaVersion != 0 && payload.SchemaVersion != service.UsageExportSchemaVersion {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported usage export schema version"})
			return
		}

		result, err := provider.ImportUsage(c.Request.Context(), payload)
		if err != nil {
			writeInternalError(c, "import usage failed", err)
			return
		}
		c.JSON(http.StatusOK, result)
	})
}

func readUsageImportPayload(c *gin.Context) (service.UsageExport, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, usageImportMaxBytes)
	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		file, err := c.FormFile("file")
		if err != nil {
			return service.UsageExport{}, err
		}
		opened, err := file.Open()
		if err != nil {
			return service.UsageExport{}, err
		}
		defer opened.Close()
		return decodeUsageImportJSON(opened)
	}
	return decodeUsageImportJSON(c.Request.Body)
}

func decodeUsageImportJSON(reader io.Reader) (service.UsageExport, error) {
	var payload service.UsageExport
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&payload); err != nil {
		return service.UsageExport{}, err
	}
	return payload, nil
}
