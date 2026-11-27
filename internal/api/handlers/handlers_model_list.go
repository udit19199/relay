package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ModelDetailIDContextKey requests selection from the final catalog.
const ModelDetailIDContextKey = "cliproxy.model_detail_id"

// WriteModelListResponse serializes the model-list payload and writes it to the Gin context.
func (h *BaseAPIHandler) WriteModelListResponse(c *gin.Context, sourceFormat string, payload any) {
	if c == nil {
		return
	}
	var body []byte
	switch v := payload.(type) {
	case []byte:
		body = cloneBytes(v)
	default:
		var errMarshal error
		body, errMarshal = json.Marshal(payload)
		if errMarshal != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": errMarshal.Error()})
			return
		}
	}

	if modelID, detail := c.Get(ModelDetailIDContextKey); detail {
		c.Writer.Header().Del("Content-Length")
		var catalog struct {
			Data   []json.RawMessage `json:"data"`
			Models []json.RawMessage `json:"models"`
		}
		if errUnmarshal := json.Unmarshal(body, &catalog); errUnmarshal != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "Invalid model catalog", "type": "api_error"}})
			return
		}
		var selected json.RawMessage
		for _, model := range append(catalog.Data, catalog.Models...) {
			var entry struct {
				ID   string `json:"id"`
				Slug string `json:"slug"`
			}
			if errUnmarshal := json.Unmarshal(model, &entry); errUnmarshal != nil {
				continue
			}
			if entry.ID == "" {
				entry.ID = entry.Slug
			}
			if entry.ID != "" && entry.ID == modelID {
				selected = model
				break
			}
		}
		if selected == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "Model not found", "type": "invalid_request_error", "code": "model_not_found"}})
			return
		}
		body = selected
	}
	if c.Writer.Header().Get("Content-Type") == "" {
		c.Header("Content-Type", "application/json; charset=utf-8")
	}
	c.Set("API_RESPONSE", cloneBytes(body))
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(body)
}
