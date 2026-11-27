package openai

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type websocketLoggingExecutor struct {
	websocketDirectCaptureExecutor
	record func(context.Context, string, []byte)
}

func (e *websocketLoggingExecutor) ExecuteStream(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	e.record(ctx, "executor", req.Payload)
	e.mu.Lock()
	if len(e.payloads) == 2 {
		e.failStatus = http.StatusBadGateway
	}
	e.mu.Unlock()
	return e.websocketDirectCaptureExecutor.ExecuteStream(ctx, auth, req, opts)
}

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
