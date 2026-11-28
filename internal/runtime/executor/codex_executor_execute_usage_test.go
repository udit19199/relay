package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/udit19199/relay/internal/config"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	coreusage "github.com/udit19199/relay/internal/service/usage"
	_ "github.com/udit19199/relay/internal/translator"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

func TestCodexExecutorExecuteRecordsFinalizedPayloadOverrideReasoningEffort(t *testing.T) {
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Override: []config.PayloadRule{
				{
					Models: []config.PayloadModelRule{
						{Name: "gpt-5.6-luna", Protocol: "codex"},
					},
					Params: map[string]any{
						"reasoning.effort": "high",
					},
				},
			},
		},
	}

	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var errRead error
		upstreamBody, errRead = io.ReadAll(r.Body)
		if errRead != nil {
			t.Errorf("read body: %v", errRead)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-luna\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":20,\"total_tokens\":30}}}\n\n")
	}))
	t.Cleanup(server.Close)

	alias := t.Name()
	capture := &codexResponseModelUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
	coreusage.RegisterNamedConsumer(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedConsumer(t.Name(), codexResponseModelNoopUsageConsumer{})
	})

	ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
	executor := NewCodexExecutor(cfg)
	resp, errExecute := executor.Execute(ctx, codexOAuthTestAuth(server.URL), coreexecutor.Request{
		Model:   "gpt-5.6-luna",
		Payload: []byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"hello"}]}`),
	}, coreexecutor.Options{SourceFormat: translatorreg.FormatOpenAI})
	if errExecute != nil {
		t.Fatalf("Execute: %v", errExecute)
	}
	if len(resp.Payload) == 0 {
		t.Fatal("Execute returned an empty response")
	}

	if effort := ojson.GetBytes(upstreamBody, "reasoning.effort").String(); effort != "high" {
		t.Fatalf("upstream payload reasoning.effort = %q, want %q", effort, "high")
	}

	record := capture.await(t)
	if record.ReasoningEffort != "high" {
		t.Fatalf("usage record ReasoningEffort = %q, want %q", record.ReasoningEffort, "high")
	}
}

func TestCodexExecutorExecuteStreamRecordsFinalizedPayloadOverrideReasoningEffort(t *testing.T) {
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Override: []config.PayloadRule{
				{
					Models: []config.PayloadModelRule{
						{Name: "gpt-5.6-luna", Protocol: "codex"},
					},
					Params: map[string]any{
						"reasoning.effort": "high",
					},
				},
			},
		},
	}

	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var errRead error
		upstreamBody, errRead = io.ReadAll(r.Body)
		if errRead != nil {
			t.Errorf("read body: %v", errRead)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-luna\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":20,\"total_tokens\":30}}}\n\n")
	}))
	t.Cleanup(server.Close)

	alias := t.Name()
	capture := &codexResponseModelUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
	coreusage.RegisterNamedConsumer(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedConsumer(t.Name(), codexResponseModelNoopUsageConsumer{})
	})

	ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
	executor := NewCodexExecutor(cfg)
	streamRes, errStream := executor.ExecuteStream(ctx, codexOAuthTestAuth(server.URL), coreexecutor.Request{
		Model:   "gpt-5.6-luna",
		Payload: []byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"hello"}]}`),
	}, coreexecutor.Options{SourceFormat: translatorreg.FormatOpenAI, Stream: true})
	if errStream != nil {
		t.Fatalf("ExecuteStream: %v", errStream)
	}
	for range streamRes.Chunks {
	}

	if effort := ojson.GetBytes(upstreamBody, "reasoning.effort").String(); effort != "high" {
		t.Fatalf("upstream payload reasoning.effort = %q, want %q", effort, "high")
	}

	record := capture.await(t)
	if record.ReasoningEffort != "high" {
		t.Fatalf("usage record ReasoningEffort = %q, want %q", record.ReasoningEffort, "high")
	}
}

func TestCodexExecutorExecuteCompactRecordsFinalizedPayloadOverrideReasoningEffort(t *testing.T) {
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Override: []config.PayloadRule{
				{
					Models: []config.PayloadModelRule{
						{Name: "gpt-5.6-luna", Protocol: "openai-response"},
					},
					Params: map[string]any{
						"reasoning.effort": "high",
					},
				},
			},
		},
	}

	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var errRead error
		upstreamBody, errRead = io.ReadAll(r.Body)
		if errRead != nil {
			t.Errorf("read body: %v", errRead)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response.compaction","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	t.Cleanup(server.Close)

	alias := t.Name()
	capture := &codexResponseModelUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
	coreusage.RegisterNamedConsumer(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedConsumer(t.Name(), codexResponseModelNoopUsageConsumer{})
	})

	ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
	executor := NewCodexExecutor(cfg)
	resp, errExecute := executor.Execute(ctx, codexOAuthTestAuth(server.URL), coreexecutor.Request{
		Model:   "gpt-5.6-luna",
		Payload: []byte(`{"model":"gpt-5.6-luna","input":[{"type":"message","role":"user","content":"history"},{"type":"compaction_trigger"}]}`),
	}, coreexecutor.Options{SourceFormat: translatorreg.FormatOpenAIResponse, Alt: "responses/compact"})
	if errExecute != nil {
		t.Fatalf("Execute: %v", errExecute)
	}
	if len(resp.Payload) == 0 {
		t.Fatal("Execute returned an empty response")
	}

	if effort := ojson.GetBytes(upstreamBody, "reasoning.effort").String(); effort != "high" {
		t.Fatalf("upstream payload reasoning.effort = %q, want %q", effort, "high")
	}

	record := capture.await(t)
	if record.ReasoningEffort != "high" {
		t.Fatalf("usage record ReasoningEffort = %q, want %q", record.ReasoningEffort, "high")
	}
}
