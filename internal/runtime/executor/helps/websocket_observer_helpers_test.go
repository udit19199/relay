package helps

import (
	"bytes"
	"context"
	"testing"

	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

func TestEmitWebSocketResponseEvent(t *testing.T) {
	var received []coreexecutor.WebSocketResponseEvent
	opts := coreexecutor.Options{
		SourceFormat: translatorreg.FormatOpenAI,
		Metadata: map[string]any{
			"request_id": "test-req-1",
			"trace_id":   "test-trace-1",
		},
		WebSocketResponseObserver: func(_ context.Context, ev coreexecutor.WebSocketResponseEvent) {
			received = append(received, ev)
		},
	}

	auth := &coreauth.Auth{
		ID:       "auth-123",
		Label:    "primary-auth",
		Provider: "codex",
		Metadata: map[string]any{
			"email": "alice@example.com",
		},
		Attributes: map[string]string{
			"auth_kind": "oauth",
		},
	}

	payload := []byte(`{"type":"codex.rate_limits","rate_limits":{"primary":{"used_percent":42}}}`)
	EmitWebSocketResponseEvent(context.Background(), opts, auth, "codex", "gpt-5.3-codex", payload)

	if len(received) != 1 {
		t.Fatalf("received %d events, want 1", len(received))
	}
	ev := received[0]
	if ev.RequestID != "test-req-1" {
		t.Fatalf("RequestID = %q, want test-req-1", ev.RequestID)
	}
	if ev.TraceID != "test-trace-1" {
		t.Fatalf("TraceID = %q, want test-trace-1", ev.TraceID)
	}
	if ev.Provider != "codex" {
		t.Fatalf("Provider = %q, want codex", ev.Provider)
	}
	if ev.AuthID != "auth-123" || ev.AuthLabel != "primary-auth" {
		t.Fatalf("Auth = (%q, %q), want (auth-123, primary-auth)", ev.AuthID, ev.AuthLabel)
	}
	if ev.AuthType != "oauth" {
		t.Fatalf("AuthType = %q, want oauth", ev.AuthType)
	}
	if ev.EventType != "codex.rate_limits" {
		t.Fatalf("EventType = %q, want codex.rate_limits", ev.EventType)
	}
	if !bytes.Equal(ev.Payload, payload) {
		t.Fatalf("Payload = %s, want %s", ev.Payload, payload)
	}
}

func TestEmitWebSocketResponseEventNilObserverNoOp(t *testing.T) {
	opts := coreexecutor.Options{}
	payload := []byte(`{"type":"codex.rate_limits"}`)
	// Should not panic
	EmitWebSocketResponseEvent(context.Background(), opts, nil, "codex", "gpt-5.3-codex", payload)
}
