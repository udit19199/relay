package executor

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	"github.com/udit19199/relay/internal/service/usage"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

// Upstream closes the SSE stream after a partial message without ever sending
// message_stop. The executor must surface a truncation error and record failed usage
// instead of closing the channel silently.
func TestClaudeExecutorStreamTruncationReportsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: message_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-5\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Abrupt clean EOF: no message_stop, no error event.
	}))
	defer server.Close()

	consumerName := "test-claude-passthrough-truncation"
	consumer := &captureClaudeUsageConsumer{
		records: make(chan usage.Record, 4),
	}
	usage.RegisterNamedConsumer(consumerName, consumer)
	t.Cleanup(func() {
		usage.RegisterNamedConsumer(consumerName, noopClaudeUsageConsumer{})
	})

	exec := NewClaudeExecutor(&config.Config{})
	auth := &coreauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := exec.ExecuteStream(ctx, auth, coreexecutor.Request{
		Model:   "claude-opus-5",
		Payload: []byte(`{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, coreexecutor.Options{
		SourceFormat:   translatorreg.FormatClaude,
		ResponseFormat: translatorreg.FormatClaude,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunkErr error
	var sawMessageStop bool
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			chunkErr = chunk.Err
			continue
		}
		if strings.Contains(string(chunk.Payload), "message_stop") {
			sawMessageStop = true
		}
	}

	if chunkErr == nil {
		t.Fatalf("expected a truncation error chunk when upstream closes before message_stop, got none (sawMessageStop=%v)", sawMessageStop)
	}
	if !strings.Contains(chunkErr.Error(), "message_stop") {
		t.Errorf("truncation error = %q, want it to mention message_stop", chunkErr.Error())
	}

	select {
	case record := <-consumer.records:
		if !record.Failed {
			t.Fatalf("expected usage record to be marked failed on truncated stream")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for failed usage record")
	}
}

// Translated stream path must also report a truncation error when upstream closes
// before message_stop.
func TestClaudeExecutorTranslatedStreamTruncationReportsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: message_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-5\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer server.Close()

	consumerName := "test-claude-translated-truncation"
	consumer := &captureClaudeUsageConsumer{
		records: make(chan usage.Record, 4),
	}
	usage.RegisterNamedConsumer(consumerName, consumer)
	t.Cleanup(func() {
		usage.RegisterNamedConsumer(consumerName, noopClaudeUsageConsumer{})
	})

	exec := NewClaudeExecutor(&config.Config{})
	auth := &coreauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := exec.ExecuteStream(ctx, auth, coreexecutor.Request{
		Model:   "claude-opus-5",
		Payload: []byte(`{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, coreexecutor.Options{
		SourceFormat:   translatorreg.FormatOpenAI,
		ResponseFormat: translatorreg.FormatOpenAI,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunkErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			chunkErr = chunk.Err
		}
	}

	if chunkErr == nil {
		t.Fatal("expected a truncation error chunk on translated path when upstream closes before message_stop, got none")
	}
	if !strings.Contains(chunkErr.Error(), "message_stop") {
		t.Errorf("truncation error = %q, want it to mention message_stop", chunkErr.Error())
	}

	select {
	case record := <-consumer.records:
		if !record.Failed {
			t.Fatalf("expected usage record to be marked failed on truncated translated stream")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for failed usage record")
	}
}

// A complete stream that ends with message_stop must not report any error.
func TestClaudeExecutorStreamCompleteNoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: message_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-5\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer server.Close()

	exec := NewClaudeExecutor(&config.Config{})
	auth := &coreauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := exec.ExecuteStream(ctx, auth, coreexecutor.Request{
		Model:   "claude-opus-5",
		Payload: []byte(`{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, coreexecutor.Options{
		SourceFormat:   translatorreg.FormatClaude,
		ResponseFormat: translatorreg.FormatClaude,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected error chunk in complete stream: %v", chunk.Err)
		}
	}
}

// OpenAICompatExecutor with ResponseFormat: FormatClaude must fail the stream on clean EOF
// when no chunk carried finish_reason, rather than synthesizing [DONE] and emitting a fake message_stop.
func TestOpenAICompatExecutorClaudeEOFBeforeFinishReasonFailsStream(t *testing.T) {
	reply := "data: {\"id\":\"chatcmpl-truncated\",\"object\":\"chat.completion.chunk\",\"created\":1773896263,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
	exec, auth, _ := newApplyPatchCompatTestExecutor(t, reply)
	request := []byte(`{"model":"test","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":true}`)

	result, errExecuteStream := exec.ExecuteStream(t.Context(), auth, coreexecutor.Request{Model: "test", Payload: request}, coreexecutor.Options{
		SourceFormat:    translatorreg.FormatClaude,
		ResponseFormat:  translatorreg.FormatClaude,
		OriginalRequest: request,
		Stream:          true,
	})
	if errExecuteStream != nil {
		t.Fatal(errExecuteStream)
	}

	var output bytes.Buffer
	var streamErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			streamErr = chunk.Err
			continue
		}
		output.Write(chunk.Payload)
	}
	if !strings.Contains(output.String(), `"type":"content_block_delta"`) {
		t.Fatalf("truncated Claude stream did not deliver its valid content frame: %q", output.String())
	}
	if streamErr == nil {
		t.Fatalf("expected error chunk on truncated OpenAI-to-Claude stream missing finish_reason, got nil; output=%q", output.String())
	}
	if strings.Contains(output.String(), "message_stop") {
		t.Fatalf("truncated OpenAI-to-Claude stream must not synthesize message_stop: %q", output.String())
	}
}

// OpenAICompatExecutor with ResponseFormat: FormatClaude must still finalize the stream on clean EOF
// when a chunk already carried a non-empty finish_reason (compatibility with providers omitting [DONE]).
func TestOpenAICompatExecutorClaudeEOFAfterFinishReasonCompletesStream(t *testing.T) {
	reply := strings.Join([]string{
		`data: {"id":"chatcmpl-eof","object":"chat.completion.chunk","created":1773896263,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-eof","object":"chat.completion.chunk","created":1773896263,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
	}, "\n")
	exec, auth, _ := newApplyPatchCompatTestExecutor(t, reply)
	request := []byte(`{"model":"test","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":true}`)

	result, errExecuteStream := exec.ExecuteStream(t.Context(), auth, coreexecutor.Request{Model: "test", Payload: request}, coreexecutor.Options{
		SourceFormat:    translatorreg.FormatClaude,
		ResponseFormat:  translatorreg.FormatClaude,
		OriginalRequest: request,
		Stream:          true,
	})
	if errExecuteStream != nil {
		t.Fatal(errExecuteStream)
	}

	var output bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error after finish_reason was received: %v", chunk.Err)
		}
		output.Write(chunk.Payload)
	}
	if !strings.Contains(output.String(), "message_stop") {
		t.Fatalf("expected message_stop when finish_reason was received before clean EOF: %q", output.String())
	}
}

// Upstream SSE error event after message_start must not be treated as a completed
// message for continuity/usage accounting: claudeMessageIDFromSSE must return empty
// and ExecuteStream must surface a terminal chunk error and failed usage record.
func TestClaudeExecutorStreamErrorEventDoesNotCommitContinuityAndMarksFailed(t *testing.T) {
	sseWithError := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_err_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-5\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n" +
		"event: error\n" +
		"data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"

	if gotID := claudeMessageIDFromSSE([]byte(sseWithError)); gotID != "" {
		t.Fatalf("claudeMessageIDFromSSE() on error stream = %q, want empty string so failed turn is not committed", gotID)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, sseWithError)
	}))
	defer server.Close()

	consumerName := "test-claude-error-event-usage"
	consumer := &captureClaudeUsageConsumer{
		records: make(chan usage.Record, 4),
	}
	usage.RegisterNamedConsumer(consumerName, consumer)
	t.Cleanup(func() {
		usage.RegisterNamedConsumer(consumerName, noopClaudeUsageConsumer{})
	})

	exec := NewClaudeExecutor(&config.Config{})
	auth := &coreauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := exec.ExecuteStream(ctx, auth, coreexecutor.Request{
		Model:   "claude-opus-5",
		Payload: []byte(`{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, coreexecutor.Options{
		SourceFormat:   translatorreg.FormatClaude,
		ResponseFormat: translatorreg.FormatClaude,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunkErr error
	var output bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			chunkErr = chunk.Err
			continue
		}
		output.Write(chunk.Payload)
	}

	if !strings.Contains(output.String(), "overloaded_error") {
		t.Fatalf("expected upstream error event payload to be forwarded, got: %q", output.String())
	}
	if chunkErr == nil {
		t.Fatal("expected terminal chunk error when upstream emits SSE error event without message_stop, got nil")
	}

	select {
	case record := <-consumer.records:
		if !record.Failed {
			t.Fatal("expected usage record to be marked failed when upstream stream ends with error event")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for failed usage record")
	}
}

func TestClaudeExecutorMultiLineDataMessageStopAndErrorEvents(t *testing.T) {
	multiLineSuccess := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\n" +
		"data: \"message\":{\"id\":\"msg_multi_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-5\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\n" +
		"data: \"message_stop\"}\n\n"

	if gotID := claudeMessageIDFromSSE([]byte(multiLineSuccess)); gotID != "msg_multi_1" {
		t.Fatalf("claudeMessageIDFromSSE() on multi-line SSE = %q, want %q", gotID, "msg_multi_1")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, multiLineSuccess)
	}))
	defer server.Close()

	exec := NewClaudeExecutor(&config.Config{})
	auth := &coreauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}
	result, err := exec.ExecuteStream(context.Background(), auth, coreexecutor.Request{
		Model:   "claude-opus-5",
		Payload: []byte(`{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, coreexecutor.Options{
		SourceFormat:   translatorreg.FormatClaude,
		ResponseFormat: translatorreg.FormatClaude,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected error on multi-line message_stop stream: %v", chunk.Err)
		}
	}

	nestedMultiLineTruncated := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_nested_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-5\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"emit\",\"input\":\n" +
		"data: {\"type\":\"message_stop\"}\n" +
		"data: }}\n\n"

	if gotID := claudeMessageIDFromSSE([]byte(nestedMultiLineTruncated)); gotID != "" {
		t.Fatalf("claudeMessageIDFromSSE() on truncated stream with nested message_stop line = %q, want empty string", gotID)
	}

	truncServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, nestedMultiLineTruncated)
	}))
	defer truncServer.Close()

	truncResult, err := exec.ExecuteStream(context.Background(), &coreauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": truncServer.URL,
	}}, coreexecutor.Request{
		Model:   "claude-opus-5",
		Payload: []byte(`{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, coreexecutor.Options{
		SourceFormat:   translatorreg.FormatClaude,
		ResponseFormat: translatorreg.FormatClaude,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var nestedChunkErr error
	for chunk := range truncResult.Chunks {
		if chunk.Err != nil {
			nestedChunkErr = chunk.Err
		}
	}
	if nestedChunkErr == nil {
		t.Fatal("expected truncation error when stream ends after multiline tool_use with nested message_stop line, got nil")
	}
}

type captureAnyProviderUsageConsumer struct {
	provider string
	records  chan usage.Record
}

func (p *captureAnyProviderUsageConsumer) HandleUsage(_ context.Context, record usage.Record) {
	if p == nil || (p.provider != "" && record.Provider != p.provider) {
		return
	}
	select {
	case p.records <- record:
	default:
	}
}

func TestTranslatedClaudeStreamTruncationPreservesBufferedUsageOnFailure(t *testing.T) {
	t.Run("openai-compatibility", func(t *testing.T) {
		reply := "data: {\"id\":\"chatcmpl-trunc-usage\",\"object\":\"chat.completion.chunk\",\"created\":1773896263,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":3,\"total_tokens\":12}}\n\n"
		exec, auth, _ := newApplyPatchCompatTestExecutor(t, reply)
		request := []byte(`{"model":"test","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":true}`)

		consumerName := "test-openai-compat-claude-truncation-usage"
		consumer := &captureAnyProviderUsageConsumer{provider: exec.Identifier(), records: make(chan usage.Record, 4)}
		usage.RegisterNamedConsumer(consumerName, consumer)
		t.Cleanup(func() {
			usage.RegisterNamedConsumer(consumerName, noopClaudeUsageConsumer{})
		})

		result, err := exec.ExecuteStream(t.Context(), auth, coreexecutor.Request{Model: "test", Payload: request}, coreexecutor.Options{
			SourceFormat:    translatorreg.FormatClaude,
			ResponseFormat:  translatorreg.FormatClaude,
			OriginalRequest: request,
			Stream:          true,
		})
		if err != nil {
			t.Fatalf("ExecuteStream() error = %v", err)
		}
		for range result.Chunks {
		}

		select {
		case record := <-consumer.records:
			if !record.Failed {
				t.Fatalf("expected failed usage record")
			}
			if record.Detail.InputTokens != 9 || record.Detail.OutputTokens != 3 {
				t.Fatalf("usage tokens = (%d, %d), want (9, 3)", record.Detail.InputTokens, record.Detail.OutputTokens)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for OpenAICompat failed usage record")
		}
	})
}

func TestClaudeExecutorStreamCancel499RecordsLastPacketUsage(t *testing.T) {
	for _, mode := range []struct {
		name   string
		format translatorreg.Format
	}{
		{name: "passthrough", format: translatorreg.FormatClaude},
		{name: "translated", format: translatorreg.FormatOpenAIResponse},
	} {
		t.Run(mode.name, func(t *testing.T) {
			deltaFlushed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				_, _ = fmt.Fprint(w, "event: message_start\n")
				_, _ = fmt.Fprint(w, `data: {"type":"message_start","message":{"id":"msg_cancel_499","type":"message","role":"assistant","content":[],"model":"claude-opus-5-5","stop_reason":null,"usage":{"input_tokens":4,"cache_creation_input_tokens":3036,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":3036},"output_tokens":1,"service_tier":"standard","inference_geo":"global","speed":"standard"}}}`+"\n\n")
				_, _ = fmt.Fprint(w, "event: content_block_start\n")
				_, _ = fmt.Fprint(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
				_, _ = fmt.Fprint(w, "event: content_block_delta\n")
				_, _ = fmt.Fprint(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`+"\n\n")
				_, _ = fmt.Fprint(w, "event: message_delta\n")
				_, _ = fmt.Fprint(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":6,"cache_creation_input_tokens":8846,"cache_read_input_tokens":3036,"output_tokens":120,"output_tokens_details":{"thinking_tokens":30},"server_tool_use":{"web_search_requests":1,"web_fetch_requests":0},"iterations":[{"type":"message","input_tokens":4,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":3036,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":3036}},{"type":"message","input_tokens":2,"output_tokens":100,"cache_read_input_tokens":3036,"cache_creation_input_tokens":5810,"cache_creation":{"ephemeral_5m_input_tokens":5810,"ephemeral_1h_input_tokens":0}}]}}`+"\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				close(deltaFlushed)
				<-r.Context().Done()
			}))
			defer server.Close()

			consumerName := "test-claude-cancel-499-" + mode.name
			consumer := &captureClaudeUsageConsumer{records: make(chan usage.Record, 4)}
			usage.RegisterNamedConsumer(consumerName, consumer)
			t.Cleanup(func() {
				usage.RegisterNamedConsumer(consumerName, noopClaudeUsageConsumer{})
			})

			exec := NewClaudeExecutor(&config.Config{})
			auth := &coreauth.Auth{Attributes: map[string]string{
				"api_key":  "key-123",
				"base_url": server.URL,
			}}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			result, err := exec.ExecuteStream(ctx, auth, coreexecutor.Request{
				Model:   "claude-opus-5-5",
				Payload: []byte(`{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
			}, coreexecutor.Options{
				SourceFormat:   mode.format,
				ResponseFormat: mode.format,
				Stream:         true,
			})
			if err != nil {
				t.Fatalf("ExecuteStream() error = %v", err)
			}

			<-deltaFlushed
			// Drain until the message_delta chunk has been processed, then cancel before message_stop.
			for chunk := range result.Chunks {
				if strings.Contains(string(chunk.Payload), "message_delta") || strings.Contains(string(chunk.Payload), "partial") {
					cancel()
					break
				}
			}
			for range result.Chunks {
			}

			select {
			case record := <-consumer.records:
				if !record.Failed || record.Fail.StatusCode != 499 {
					t.Fatalf("expected failed=true status=499 on canceled stream, got failed=%v fail=%+v", record.Failed, record.Fail)
				}
				if record.Detail.InputTokens != 6 || record.Detail.OutputTokens != 120 || record.Detail.ReasoningTokens != 30 || record.Detail.CacheReadTokens != 3036 || record.Detail.CacheCreationTokens != 8846 {
					t.Fatalf("unexpected last-packet token counts on 499 cancel: %+v", record.Detail)
				}
				if record.Detail.CacheCreation5mTokens == nil || *record.Detail.CacheCreation5mTokens != 5810 || record.Detail.CacheCreation1hTokens == nil || *record.Detail.CacheCreation1hTokens != 3036 {
					t.Fatalf("expected 5m=5810 1h=3036 on 499 cancel, got 5m=%v 1h=%v", record.Detail.CacheCreation5mTokens, record.Detail.CacheCreation1hTokens)
				}
				if record.Detail.WebSearchRequests == nil || *record.Detail.WebSearchRequests != 1 {
					t.Fatalf("expected WebSearchRequests=1 on 499 cancel, got %v", record.Detail.WebSearchRequests)
				}
				if len(record.Detail.IterationSlice()) != 2 {
					t.Fatalf("expected 2 iterations on 499 cancel, got %d", len(record.Detail.IterationSlice()))
				}
			case <-time.After(3 * time.Second):
				t.Fatal("timed out waiting for 499 canceled usage record")
			}
		})
	}
}
