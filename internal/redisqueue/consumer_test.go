package redisqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	internallogging "github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	coresession "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/session"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestUsageQueueConsumerPayloadIncludesStableFieldsAndSuccess(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-request-id")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithClientRequestMetadata(ctx, internallogging.ClientRequestMetadata{
			ClientIP:         "192.0.2.10",
			ResolvedClientIP: "203.0.113.5",
			XForwardedFor:    "203.0.113.5, 198.51.100.8",
			UserAgent:        "test-client/1.0",
		})
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)
		responseHeaders := http.Header{}
		responseHeaders.Add("X-Upstream-Request-Id", "upstream-req-1")
		responseHeaders.Add("Retry-After", "30")

		consumer := &usageQueueConsumer{}
		consumer.HandleUsage(ctx, coreusage.Record{
			Provider:             "openai",
			ExecutorType:         "XAIExecutor",
			Model:                "gpt-5.4",
			Alias:                "client-gpt",
			APIKey:               "test-key",
			AuthIndex:            "0",
			AccessTokenSHA256:    "token-version-hash",
			AuthType:             "apikey",
			Source:               "user@example.com",
			ReasoningEffort:      "medium",
			ServiceTier:          "auto",
			EffectiveServiceTier: "priority",
			ResponseServiceTier:  "default",
			ResponseModel:        "gpt-5.6-luna",
			Generate:             coreusage.GenerateFlag(true),
			RequestedAt:          time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:              1500 * time.Millisecond,
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
			ResponseHeaders: responseHeaders.Clone(),
		})
		responseHeaders.Set("Retry-After", "999")

		payload := popSinglePayload(t)
		requireStringField(t, payload, "provider", "openai")
		requireStringField(t, payload, "executor_type", "XAIExecutor")
		requireStringField(t, payload, "model", "gpt-5.4")
		requireStringField(t, payload, "alias", "client-gpt")
		requireStringField(t, payload, "endpoint", "POST /v1/chat/completions")
		requireStringField(t, payload, "auth_type", "apikey")
		requireStringField(t, payload, "access_token_sha256", "token-version-hash")
		requireMissingField(t, payload, "user_api_key")
		requireStringField(t, payload, "request_id", "ctx-request-id")
		requireStringField(t, payload, "client_ip", "192.0.2.10")
		requireStringField(t, payload, "resolved_client_ip", "203.0.113.5")
		requireStringField(t, payload, "x_forwarded_for", "203.0.113.5, 198.51.100.8")
		requireStringField(t, payload, "user_agent", "test-client/1.0")
		requireStringField(t, payload, "reasoning_effort", "medium")
		requireStringField(t, payload, "service_tier", "auto")
		requireStringField(t, payload, "effective_service_tier", "priority")
		requireMissingField(t, payload, "request_service_tier")
		requireStringField(t, payload, "response_service_tier", "default")
		requireStringField(t, payload, "response_model", "gpt-5.6-luna")
		requireIntField(t, payload, "accounting_version", coreusage.TokenAccountingSchemaVersion)
		requireTokenBreakdown(t, payload, coreusage.TokenAccountingQualityComplete, 30)
		requireTokensBoolField(t, payload, "cache_read_tokens_present", true)
		requireHeaderField(t, payload, "response_headers", "X-Upstream-Request-Id", []string{"upstream-req-1"})
		requireHeaderField(t, payload, "response_headers", "Retry-After", []string{"30"})
		requireBoolField(t, payload, "failed", false)
		requireBoolField(t, payload, "generate", true)
		requireBoolField(t, payload, "stream", false)
		requireFailField(t, payload, http.StatusOK, "")
	})
}

func TestUsageQueueConsumerNormalizesDirectSDKUsageByProvider(t *testing.T) {
	tests := []struct {
		provider  string
		wantTotal int
	}{
		{provider: "openai", wantTotal: 130},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			withEnabledQueue(t, func() {
				ctx := internallogging.WithResponseStatusHolder(context.Background())
				internallogging.SetResponseStatus(ctx, http.StatusOK)

				(&usageQueueConsumer{}).HandleUsage(ctx, coreusage.Record{
					Provider: tt.provider,
					Model:    "direct-sdk-model",
					Detail: coreusage.Detail{
						InputTokens:     100,
						OutputTokens:    30,
						ReasoningTokens: 12,
					},
				})

				payload := popSinglePayload(t)
				requireIntField(t, requireTokensPayload(t, payload), "total_tokens", tt.wantTotal)
				requireTokenBreakdown(t, payload, coreusage.TokenAccountingQualityComplete, int64(tt.wantTotal))
			})
		})
	}
}

func TestUsageQueueConsumerPayloadIncludesGenerateFalse(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithResponseStatusHolder(context.Background())
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		(&usageQueueConsumer{}).HandleUsage(ctx, coreusage.Record{
			Provider: "openai",
			Model:    "gpt-5.4",
			Generate: coreusage.GenerateFlag(false),
			Detail: coreusage.Detail{
				InputTokens: 1,
				TotalTokens: 1,
			},
		})

		payload := popSinglePayload(t)
		requireBoolField(t, payload, "generate", false)
	})
}

func TestUsageQueueConsumerPayloadDefaultsGenerateTrueWhenOmitted(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithResponseStatusHolder(context.Background())
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		// Legacy callers construct usage.Record without Generate; omission must publish as true.
		(&usageQueueConsumer{}).HandleUsage(ctx, coreusage.Record{
			Provider: "openai",
			Model:    "gpt-5.4",
			Detail: coreusage.Detail{
				InputTokens: 1,
				TotalTokens: 1,
			},
		})

		payload := popSinglePayload(t)
		requireBoolField(t, payload, "generate", true)
	})
}

func TestUsageQueueConsumerPublishesStreamFlag(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream_true", false: "stream_false"}[stream], func(t *testing.T) {
			withEnabledQueue(t, func() {
				ctx := internallogging.WithResponseStatusHolder(context.Background())
				internallogging.SetResponseStatus(ctx, http.StatusOK)

				(&usageQueueConsumer{}).HandleUsage(ctx, coreusage.Record{
					Provider: "openai",
					Model:    "gpt-5.4",
					Stream:   stream,
					Detail: coreusage.Detail{
						InputTokens: 1,
						TotalTokens: 1,
					},
				})

				payload := popSinglePayload(t)
				requireBoolField(t, payload, "stream", stream)
			})
		})
	}
}

func TestUsageQueueConsumerPreservesLegacyCachedOnlyUsage(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithResponseStatusHolder(context.Background())
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		(&usageQueueConsumer{}).HandleUsage(ctx, coreusage.Record{
			Provider: "openai",
			Model:    "gpt-5.4",
			Detail: coreusage.Detail{
				CachedTokens: 13,
			},
		})

		payload := popSinglePayload(t)
		requireTokensBoolField(t, payload, "cache_read_tokens_present", true)
		tokens := requireTokensPayload(t, payload)
		requireIntField(t, tokens, "cache_read_tokens", 13)
		requireIntField(t, tokens, "total_tokens", 13)
		requireTokenBreakdown(t, payload, coreusage.TokenAccountingQualityUnclassified, 13)
	})
}

func TestUsageQueueConsumerEmitsSingleCanonicalAutoTier(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := coreusage.WithServiceTier(context.Background(), coreusage.AutoServiceTier)
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		(&usageQueueConsumer{}).HandleUsage(ctx, coreusage.Record{
			Provider: "openai",
			Model:    "gpt-5.4",
			Detail: coreusage.Detail{
				InputTokens: 1,
				TotalTokens: 1,
			},
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "service_tier", "auto")
		requireMissingField(t, payload, "effective_service_tier")
		requireMissingField(t, payload, "request_service_tier")
	})
}

func TestUsageQueueConsumerAcceptsDeprecatedRequestTierRecordField(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithResponseStatusHolder(context.Background())
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		(&usageQueueConsumer{}).HandleUsage(ctx, coreusage.Record{
			Provider:           "openai",
			Model:              "gpt-5.4",
			RequestServiceTier: "priority",
			Detail:             coreusage.Detail{InputTokens: 1, TotalTokens: 1},
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "service_tier", "priority")
		requireMissingField(t, payload, "request_service_tier")
	})
}

func TestUsageQueueConsumerAsyncUsesRecordResponseHeaders(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-request-id")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		ctx = internallogging.WithResponseHeadersHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)
		initialHeaders := http.Header{}
		initialHeaders.Set("X-Upstream-Request-Id", "upstream-req-1")
		internallogging.SetResponseHeaders(ctx, initialHeaders)

		mgr := coreusage.NewManager(16)
		defer mgr.Stop()

		mgr.Register(consumerFunc(func(ctx context.Context, _ coreusage.Record) {
			nextHeaders := http.Header{}
			nextHeaders.Set("X-Upstream-Request-Id", "upstream-req-2")
			internallogging.SetResponseHeaders(ctx, nextHeaders)
		}))
		mgr.Register(&usageQueueConsumer{})

		mgr.Publish(ctx, coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.4",
			Alias:       "client-gpt",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     1500 * time.Millisecond,
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
			ResponseHeaders: internallogging.GetResponseHeaders(ctx),
		})

		payload := waitForSinglePayload(t, 2*time.Second)
		requireHeaderField(t, payload, "response_headers", "X-Upstream-Request-Id", []string{"upstream-req-1"})
	})
}

func TestUsageQueueConsumerPayloadIncludesStableFieldsAndFailureAndGinRequestID(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "gin-request-id")
		ctx = internallogging.WithEndpoint(ctx, "GET /v1/responses")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusInternalServerError)

		consumer := &usageQueueConsumer{}
		consumer.HandleUsage(ctx, coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.4-mini",
			Alias:       "client-mini",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     2500 * time.Millisecond,
			Fail: coreusage.Failure{
				StatusCode: http.StatusInternalServerError,
				Body:       "upstream failed",
			},
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "provider", "openai")
		requireStringField(t, payload, "model", "gpt-5.4-mini")
		requireStringField(t, payload, "alias", "client-mini")
		requireStringField(t, payload, "endpoint", "GET /v1/responses")
		requireStringField(t, payload, "auth_type", "apikey")
		requireMissingField(t, payload, "user_api_key")
		requireStringField(t, payload, "request_id", "gin-request-id")
		requireBoolField(t, payload, "failed", true)
		requireFailField(t, payload, http.StatusInternalServerError, "upstream failed")
	})
}

func TestUsageQueueConsumerAsyncIgnoresRecycledGinContext(t *testing.T) {
	withEnabledQueue(t, func() {
		ginCtx := newTestGinContext(t, http.MethodPost, "/v1/chat/completions", http.StatusOK)
		ctx := context.WithValue(context.Background(), "gin", ginCtx)
		ctx = internallogging.WithRequestID(ctx, "ctx-request-id")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusInternalServerError)

		mgr := coreusage.NewManager(16)
		defer mgr.Stop()

		mgr.Register(consumerFunc(func(_ context.Context, _ coreusage.Record) {
			ginCtx.Request = httptest.NewRequest(http.MethodGet, "http://example.com/v1/responses", nil)
			ginCtx.Status(http.StatusOK)
		}))
		mgr.Register(&usageQueueConsumer{})

		mgr.Publish(ctx, coreusage.Record{
			Provider:    "openai",
			Model:       "gpt-5.4",
			Alias:       "client-gpt",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     1500 * time.Millisecond,
			Fail: coreusage.Failure{
				StatusCode: http.StatusBadGateway,
				Body:       "bad gateway",
			},
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		})

		payload := waitForSinglePayload(t, 2*time.Second)
		requireStringField(t, payload, "endpoint", "POST /v1/chat/completions")
		requireStringField(t, payload, "alias", "client-gpt")
		requireMissingField(t, payload, "user_api_key")
		requireStringField(t, payload, "request_id", "ctx-request-id")
		requireBoolField(t, payload, "failed", true)
		requireFailField(t, payload, http.StatusBadGateway, "bad gateway")
	})
}

func withEnabledQueue(t *testing.T, fn func()) {
	t.Helper()

	prevQueueEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()

	SetEnabled(false)
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)

	defer func() {
		SetEnabled(false)
		SetEnabled(prevQueueEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	}()

	fn()
}

func newTestGinContext(t *testing.T, method, path string, status int) *gin.Context {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(method, "http://example.com"+path, nil)
	if status != 0 {
		ginCtx.Status(status)
	}
	return ginCtx
}

func popSinglePayload(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	items := PopOldest(10)
	if len(items) != 1 {
		t.Fatalf("PopOldest() items = %d, want 1", len(items))
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(items[0], &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func waitForSinglePayload(t *testing.T, timeout time.Duration) map[string]json.RawMessage {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		items := PopOldest(10)
		if len(items) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if len(items) != 1 {
			t.Fatalf("PopOldest() items = %d, want 1", len(items))
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(items[0], &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		return payload
	}
	t.Fatalf("timeout waiting for queued payload")
	return nil
}

func requireStringField(t *testing.T, payload map[string]json.RawMessage, key, want string) {
	t.Helper()

	raw, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing %q", key)
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func requireIntField(t *testing.T, payload map[string]json.RawMessage, key string, want int) {
	t.Helper()

	raw, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing %q", key)
	}
	var got int
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", key, got, want)
	}
}

func requireTokenBreakdown(t *testing.T, payload map[string]json.RawMessage, quality coreusage.TokenAccountingQuality, total int64) {
	t.Helper()

	raw, ok := payload["token_breakdown"]
	if !ok {
		t.Fatal("payload missing token_breakdown")
	}
	var breakdown coreusage.TokenBreakdown
	if err := json.Unmarshal(raw, &breakdown); err != nil {
		t.Fatalf("unmarshal token_breakdown: %v", err)
	}
	if !breakdown.Valid() || breakdown.Quality != quality || breakdown.TotalTokens != total {
		t.Fatalf("token_breakdown = %+v, want quality=%s total=%d", breakdown, quality, total)
	}
}

func requireMissingField(t *testing.T, payload map[string]json.RawMessage, key string) {
	t.Helper()

	if _, ok := payload[key]; ok {
		t.Fatalf("payload unexpectedly contains %q", key)
	}
}

type consumerFunc func(context.Context, coreusage.Record)

func (fn consumerFunc) HandleUsage(ctx context.Context, record coreusage.Record) {
	fn(ctx, record)
}

func requireBoolField(t *testing.T, payload map[string]json.RawMessage, key string, want bool) {
	t.Helper()

	raw, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing %q", key)
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s = %t, want %t", key, got, want)
	}
}

func requireTokensPayload(t *testing.T, payload map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	raw, ok := payload["tokens"]
	if !ok {
		t.Fatal("payload missing tokens")
	}
	var tokens map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(raw, &tokens); errUnmarshal != nil {
		t.Fatalf("unmarshal tokens: %v", errUnmarshal)
	}
	return tokens
}

func requireTokensBoolField(t *testing.T, payload map[string]json.RawMessage, key string, want bool) {
	t.Helper()
	requireBoolField(t, requireTokensPayload(t, payload), key, want)
}

func requireFailField(t *testing.T, payload map[string]json.RawMessage, wantStatus int, wantBody string) {
	t.Helper()

	raw, ok := payload["fail"]
	if !ok {
		t.Fatalf("payload missing %q", "fail")
	}
	var got struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal fail: %v", err)
	}
	if got.StatusCode != wantStatus || got.Body != wantBody {
		t.Fatalf("fail = {status_code:%d body:%q}, want {status_code:%d body:%q}", got.StatusCode, got.Body, wantStatus, wantBody)
	}
}

func requireHeaderField(t *testing.T, payload map[string]json.RawMessage, field, key string, want []string) {
	t.Helper()

	raw, ok := payload[field]
	if !ok {
		t.Fatalf("payload missing %q", field)
	}
	var headers map[string][]string
	if err := json.Unmarshal(raw, &headers); err != nil {
		t.Fatalf("unmarshal %q: %v", field, err)
	}
	got, ok := headers[key]
	if !ok {
		t.Fatalf("%s missing header %q", field, key)
	}
	if len(got) != len(want) {
		t.Fatalf("%s[%q] = %v, want %v", field, key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%q] = %v, want %v", field, key, got, want)
		}
	}
}

func TestUsageQueueConsumerPayloadIncludesExplicitSessionHierarchy(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-session-req-1")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithClientRequestMetadata(ctx, internallogging.ClientRequestMetadata{
			ClientIP:        "192.0.2.10",
			SessionID:       "slot:pi-worker-1",
			ParentSessionID: "slot:pi-main-root",
		})
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		consumer := &usageQueueConsumer{}
		consumer.HandleUsage(ctx, coreusage.Record{
			Provider:        "openai",
			Model:           "gpt-5.6-sol",
			APIKey:          "test-key",
			SessionID:       "slot:pi-worker-1",
			ParentSessionID: "slot:pi-main-root",
			RequestedAt:     time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
			Latency:         5000 * time.Millisecond,
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "request_id", "ctx-session-req-1")
		wantSession := coresession.NormalizeToCanonicalUUID("slot:pi-worker-1")
		wantParent := coresession.NormalizeToCanonicalUUID("slot:pi-main-root")
		requireStringField(t, payload, "session_id", wantSession)
		requireStringField(t, payload, "parent_session_id", wantParent)
		if len(wantSession) != 36 || wantSession[14] != '8' {
			t.Fatalf("expected 36-char UUIDv8 for session_id, got %q", wantSession)
		}
		if len(wantParent) != 36 || wantParent[14] != '8' {
			t.Fatalf("expected 36-char UUIDv8 for parent_session_id, got %q", wantParent)
		}
	})
}

func TestUsageQueueConsumerPayloadNormalizesPrefixedNativeUUID(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-native-uuid-1")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		consumer := &usageQueueConsumer{}
		consumer.HandleUsage(ctx, coreusage.Record{
			Provider:        "codex",
			Model:           "gpt-5.6-sol",
			APIKey:          "test-key",
			SessionID:       "codex:01a07e72-c84d-7fd3-8207-d217b41cc649",
			ParentSessionID: "codex:01a07e71-a1b2-7c3d-98e1-f23456789abc",
			RequestedAt:     time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
			Latency:         1000 * time.Millisecond,
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "session_id", "01a07e72-c84d-7fd3-8207-d217b41cc649")
		requireStringField(t, payload, "parent_session_id", "01a07e71-a1b2-7c3d-98e1-f23456789abc")
	})
}

func TestUsageQueueConsumerPayloadPreventsSelfReferentialLoop(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-loop-req-1")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		consumer := &usageQueueConsumer{}
		consumer.HandleUsage(ctx, coreusage.Record{
			Provider:        "openai",
			Model:           "gpt-5.6-sol",
			APIKey:          "test-key",
			SessionID:       "loop-sess-1",
			ParentSessionID: "loop-sess-1",
			RequestedAt:     time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
			Latency:         1000 * time.Millisecond,
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "request_id", "ctx-loop-req-1")
		wantSession := coresession.NormalizeToCanonicalUUID("loop-sess-1")
		requireStringField(t, payload, "session_id", wantSession)
		if _, exists := payload["parent_session_id"]; exists {
			t.Fatalf("expected parent_session_id to be omitted on self-referential loop, got %s", payload["parent_session_id"])
		}
	})
}

func TestUsageQueueConsumerPayloadSameOriginFallback(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-same-origin-1")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithClientRequestMetadata(ctx, internallogging.ClientRequestMetadata{
			SessionID:       "old-root",
			ParentSessionID: "old-parent",
		})
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		consumer := &usageQueueConsumer{}
		// Record explicitly defines an independent root session without parent.
		// It should NOT pick up old-parent from context.
		consumer.HandleUsage(ctx, coreusage.Record{
			Provider:        "openai",
			Model:           "gpt-5.6-sol",
			APIKey:          "test-key",
			SessionID:       "new-custom-root",
			ParentSessionID: "",
			RequestedAt:     time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
			Latency:         1000 * time.Millisecond,
		})

		payload := popSinglePayload(t)
		wantSession := coresession.NormalizeToCanonicalUUID("new-custom-root")
		requireStringField(t, payload, "session_id", wantSession)
		if _, exists := payload["parent_session_id"]; exists {
			t.Fatalf("expected parent_session_id to be omitted when record is independent root, got %s", payload["parent_session_id"])
		}
	})
}

func TestUsageQueueConsumer_SchemeB_ExecutionIDAndTraceID(t *testing.T) {
	prevEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		SetEnabled(prevEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	consumer := &usageQueueConsumer{}
	ctx := internallogging.WithRequestID(context.Background(), "000000ab")
	execUUID := "12345678-1234-4234-8234-123456789abc"

	consumer.HandleUsage(ctx, coreusage.Record{
		RequestID: execUUID,
		TraceID:   "000000ab",
		Provider:  "openai",
		Model:     "gpt-5.4",
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	})

	payload := popSinglePayload(t)
	// Scheme B: request_id preserves the 8-character hex trace ID
	requireStringField(t, payload, "request_id", "000000ab")
	// Scheme B: execution_id carries the UUID v4 execution instance ID
	requireStringField(t, payload, "execution_id", execUUID)
	// Scheme B: trace_id explicitly carries the 8-character hex trace ID
	requireStringField(t, payload, "trace_id", "000000ab")
}

func TestUsageQueueConsumer_SchemeB_StrictLegacyRequestIDPreservation(t *testing.T) {
	prevEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		SetEnabled(prevEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	consumer := &usageQueueConsumer{}
	ctx := internallogging.WithRequestID(context.Background(), "legacy-log-id")
	execUUID := "12345678-1234-4234-8234-123456789abc"

	consumer.HandleUsage(ctx, coreusage.Record{
		RequestID: execUUID,
		TraceID:   "custom-trace-id",
		Provider:  "openai",
		Model:     "gpt-5.4",
		Detail: coreusage.Detail{
			InputTokens: 5,
		},
	})

	payload := popSinglePayload(t)
	// request_id strictly preserves legacy GetRequestID(ctx)
	requireStringField(t, payload, "request_id", "legacy-log-id")
	// execution_id carries the UUID v4
	requireStringField(t, payload, "execution_id", execUUID)
	// trace_id reflects the record.TraceID
	requireStringField(t, payload, "trace_id", "custom-trace-id")
}

func TestUsageQueueConsumerPayloadDistinguishesNativeKey(t *testing.T) {
	withEnabledQueue(t, func() {
		t.Run("native_key", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(w)
			ginCtx.Set("userApiKey", "sk-native-key-123")
			ginCtx.Set("accessProvider", "config-inline")

			ctx := context.WithValue(context.Background(), "gin", ginCtx)
			ctx = internallogging.WithRequestID(ctx, "req-native-1")

			consumer := &usageQueueConsumer{}
			consumer.HandleUsage(ctx, coreusage.Record{
				Provider: "openai",
				Model:    "gpt-5.4",
				APIKey:   "sk-native-key-123",
			})

			payload := popSinglePayload(t)
			requireStringField(t, payload, "api_key", "sk-native-key-123")
			requireBoolField(t, payload, "is_native_key", true)
			requireStringField(t, payload, "access_provider", "config-inline")
		})

		t.Run("consumer_key", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(w)
			ginCtx.Set("userApiKey", "consumer-principal-456")
			ginCtx.Set("accessProvider", "custom-auth-consumer")

			ctx := context.WithValue(context.Background(), "gin", ginCtx)
			ctx = internallogging.WithRequestID(ctx, "req-consumer-1")

			consumer := &usageQueueConsumer{}
			consumer.HandleUsage(ctx, coreusage.Record{
				Provider: "openai",
				Model:    "gpt-5.4",
				APIKey:   "consumer-principal-456",
			})

			payload := popSinglePayload(t)
			requireStringField(t, payload, "api_key", "consumer-principal-456")
			requireBoolField(t, payload, "is_native_key", false)
			requireStringField(t, payload, "access_provider", "custom-auth-consumer")
		})

		t.Run("record_explicit_native", func(t *testing.T) {
			ctx := internallogging.WithRequestID(context.Background(), "req-explicit-1")

			consumer := &usageQueueConsumer{}
			consumer.HandleUsage(ctx, coreusage.Record{
				Provider:       "openai",
				Model:          "gpt-5.4",
				APIKey:         "sk-explicit-native",
				IsNativeKey:    true,
				AccessProvider: "config-inline",
			})

			payload := popSinglePayload(t)
			requireStringField(t, payload, "api_key", "sk-explicit-native")
			requireBoolField(t, payload, "is_native_key", true)
			requireStringField(t, payload, "access_provider", "config-inline")
		})

		t.Run("unauthenticated", func(t *testing.T) {
			ctx := internallogging.WithRequestID(context.Background(), "req-unauth-1")

			consumer := &usageQueueConsumer{}
			consumer.HandleUsage(ctx, coreusage.Record{
				Provider: "openai",
				Model:    "gpt-5.4",
			})

			payload := popSinglePayload(t)
			requireBoolField(t, payload, "is_native_key", false)
			requireMissingField(t, payload, "access_provider")
		})
	})
}

func TestUsageQueueConsumerEmitsFailCode(t *testing.T) {
	withEnabledQueue(t, func() {
		ginCtx := newTestGinContext(t, http.MethodPost, "/v1/chat/completions", http.StatusOK)
		ctx := context.WithValue(context.Background(), "gin", ginCtx)

		consumer := &usageQueueConsumer{}
		consumer.HandleUsage(ctx, coreusage.Record{
			Provider:    "gemini",
			Model:       "gemini-3.1-pro-preview",
			AuthIndex:   "0",
			AuthType:    "apikey",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Failed:      true,
			Fail: coreusage.Failure{
				StatusCode: http.StatusBadGateway,
				Body:       "upstream stream closed before finishReason",
				Code:       coreusage.FailureStreamTruncatedCode,
			},
		})

		payload := popSinglePayload(t)
		raw, ok := payload["fail"]
		if !ok {
			t.Fatal("payload missing fail")
		}
		var got struct {
			StatusCode int    `json:"status_code"`
			Body       string `json:"body"`
			Code       string `json:"code"`
		}
		if errUnmarshal := json.Unmarshal(raw, &got); errUnmarshal != nil {
			t.Fatalf("unmarshal fail: %v", errUnmarshal)
		}
		if got.Code != coreusage.FailureStreamTruncatedCode {
			t.Fatalf("fail.code = %q, want %q", got.Code, coreusage.FailureStreamTruncatedCode)
		}
		if got.StatusCode != http.StatusBadGateway {
			t.Fatalf("fail.status_code = %d, want %d", got.StatusCode, http.StatusBadGateway)
		}
	})
}

func TestUsageQueueConsumerPublishesAnthropicStructuredUsageDetails(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "claude-req-1")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/messages")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		consumer := &usageQueueConsumer{}
		consumer.HandleUsage(ctx, coreusage.Record{
			Provider:            "claude",
			ExecutorType:        "ClaudeExecutor",
			Model:               "claude-opus-5-5",
			ResponseServiceTier: "standard",
			Detail: coreusage.Detail{
				InputTokens:           6,
				OutputTokens:          483,
				ReasoningTokens:       45,
				CachedTokens:          3036,
				CacheReadTokens:       3036,
				CacheCreationTokens:   8846,
				TotalTokens:           12371,
				CacheCreation5mTokens: coreusage.Int64Ptr(5810),
				CacheCreation1hTokens: coreusage.Int64Ptr(3036),
				CacheDetails: &coreusage.CacheDetails{
					Ephemeral5mInputTokens: coreusage.Int64Ptr(5810),
					Ephemeral1hInputTokens: coreusage.Int64Ptr(3036),
					ByTTL:                  map[string]int64{"5m": 5810, "1h": 3036},
				},
				WebSearchRequests: coreusage.Int64Ptr(1),
				ToolUsageDetails: &coreusage.ToolUsageDetails{
					WebSearchCalls: coreusage.Int64Ptr(1),
					WebFetchCalls:  coreusage.Int64Ptr(0),
				},
				Iterations: coreusage.IterationsPtr([]coreusage.IterationUsage{
					{
						Type:                     "message",
						InputTokens:              coreusage.Int64Ptr(4),
						OutputTokens:             coreusage.Int64Ptr(53),
						CacheReadInputTokens:     coreusage.Int64Ptr(0),
						CacheCreationInputTokens: coreusage.Int64Ptr(3036),
						CacheCreation5mTokens:    coreusage.Int64Ptr(0),
						CacheCreation1hTokens:    coreusage.Int64Ptr(3036),
					},
					{
						Type:                     "message",
						InputTokens:              coreusage.Int64Ptr(2),
						OutputTokens:             coreusage.Int64Ptr(430),
						CacheReadInputTokens:     coreusage.Int64Ptr(3036),
						CacheCreationInputTokens: coreusage.Int64Ptr(5810),
						CacheCreation5mTokens:    coreusage.Int64Ptr(5810),
						CacheCreation1hTokens:    coreusage.Int64Ptr(0),
					},
				}),
				Speed:        "standard",
				InferenceGeo: "global",
			},
		})

		payload := popSinglePayload(t)
		tokens := requireTokensPayload(t, payload)
		requireIntField(t, tokens, "cache_creation_5m_tokens", 5810)
		requireIntField(t, tokens, "cache_creation_1h_tokens", 3036)
		requireMissingField(t, tokens, "cache_creation_30m_tokens")
		requireIntField(t, payload, "web_search_requests", 1)
		requireStringField(t, payload, "speed", "standard")
		requireStringField(t, payload, "inference_geo", "global")
		if _, ok := payload["iterations"]; !ok {
			t.Fatal("expected iterations in payload")
		}
		if _, ok := payload["tool_usage_details"]; !ok {
			t.Fatal("expected tool_usage_details in payload")
		}
	})
}
