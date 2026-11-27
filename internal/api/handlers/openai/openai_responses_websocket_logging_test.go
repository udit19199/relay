package openai

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/logging"
	"github.com/udit19199/relay/internal/runtime/executor/helps"
)

func TestResponsesWebsocketRejectedUpgradeKeepsErrorCapture(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("Connection", "Upgrade")
	c.Request.Header.Set("Upgrade", "websocket")
	// Missing handshake headers make the upgrade fail before a socket exists.
	(&OpenAIResponsesAPIHandler{}).ResponsesWebsocket(c)
	if c.Writer.Status() < http.StatusBadRequest {
		t.Fatal("invalid WebSocket handshake was accepted")
	}
	ctx := context.WithValue(c.Request.Context(), "gin", c)
	helps.RecordAPIRequest(ctx, &config.Config{}, helps.UpstreamRequestLog{Body: []byte("handshake-error")})
	value, _ := c.Get(logging.DeferredAPIRequestContextKey)
	requests, _ := value.([]logging.DeferredAPIRequest)
	if len(requests) != 1 || !bytes.Contains(requests[0](), []byte("handshake-error")) {
		t.Fatal("failed upgrade lost ordinary HTTP error capture")
	}
}
