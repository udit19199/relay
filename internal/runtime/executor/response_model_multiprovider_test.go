package executor

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/runtime/executor/helps"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	coreusage "github.com/udit19199/relay/internal/service/usage"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
	"google.golang.org/protobuf/encoding/protowire"
)

type multiProviderUsageCapture struct {
	alias   string
	records chan coreusage.Record
}

func (c *multiProviderUsageCapture) HandleUsage(_ context.Context, record coreusage.Record) {
	if record.Alias != c.alias {
		return
	}
	select {
	case c.records <- record:
	default:
	}
}

type multiProviderNoopUsageConsumer struct{}

func (multiProviderNoopUsageConsumer) HandleUsage(context.Context, coreusage.Record) {}

func (c *multiProviderUsageCapture) await(t *testing.T) coreusage.Record {
	t.Helper()
	select {
	case record := <-c.records:
		return record
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the usage record")
		return coreusage.Record{}
	}
}

func TestClaudeUsageRecordCarriesResponseModel(t *testing.T) {
	const alias = "claude-response-model-test"
	capture := &multiProviderUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
	coreusage.RegisterNamedConsumer(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedConsumer(t.Name(), multiProviderNoopUsageConsumer{})
	})

	ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
	auth := &coreauth.Auth{ID: "claude-auth-1", Index: "auth-claude-1", Provider: "claude"}
	reporter := helps.NewExecutorUsageReporter(ctx, NewClaudeExecutor(&config.Config{}), "claude-opus-5", auth)

	reporter.ObserveResponseModel([]byte(`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5"}}`))
	reporter.Publish(ctx, coreusage.Detail{InputTokens: 10, OutputTokens: 20})

	record := capture.await(t)
	if record.Model != "claude-opus-5" {
		t.Fatalf("record model = %q, want claude-opus-5", record.Model)
	}
	if record.ResponseModel != "claude-sonnet-5" {
		t.Fatalf("record response model = %q, want claude-sonnet-5", record.ResponseModel)
	}
}

func TestOpenAICompatUsageRecordCarriesResponseModel(t *testing.T) {
	const alias = "openai-response-model-test"
	capture := &multiProviderUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
	coreusage.RegisterNamedConsumer(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedConsumer(t.Name(), multiProviderNoopUsageConsumer{})
	})

	ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
	auth := &coreauth.Auth{ID: "openai-auth-1", Index: "auth-openai-1", Provider: "openai"}
	reporter := helps.NewExecutorUsageReporter(ctx, NewOpenAICompatExecutor("openai", &config.Config{}), "gpt-4o", auth)

	reporter.ObserveResponseModel([]byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"gpt-4o-2024-08-06","choices":[]}`))
	reporter.Publish(ctx, coreusage.Detail{InputTokens: 30, OutputTokens: 40})

	record := capture.await(t)
	if record.Model != "gpt-4o" {
		t.Fatalf("record model = %q, want gpt-4o", record.Model)
	}
	if record.ResponseModel != "gpt-4o-2024-08-06" {
		t.Fatalf("record response model = %q, want gpt-4o-2024-08-06", record.ResponseModel)
	}
}

func TestDevinStreamRecordsResponseModelFromUsage(t *testing.T) {
	const alias = "devin-stream-response-model-test"
	capture := &multiProviderUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
	coreusage.RegisterNamedConsumer(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedConsumer(t.Name(), multiProviderNoopUsageConsumer{})
	})

	ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
	auth := &coreauth.Auth{ID: "devin-auth-1", Index: "auth-devin-1", Provider: "devin"}
	exec := NewDevinExecutor(&config.Config{})
	reporter := helps.NewExecutorUsageReporter(ctx, exec, "devin/swe-2", auth)

	var f7 []byte
	f7 = protowire.AppendTag(f7, 2, protowire.VarintType)
	f7 = protowire.AppendVarint(f7, 10)
	f7 = protowire.AppendTag(f7, 3, protowire.VarintType)
	f7 = protowire.AppendVarint(f7, 20)
	f7 = protowire.AppendTag(f7, 9, protowire.BytesType)
	f7 = protowire.AppendString(f7, "gpt-5-6-luna-low")

	var f1 []byte
	f1 = protowire.AppendTag(f1, 7, protowire.BytesType)
	f1 = protowire.AppendBytes(f1, f7)

	var buf bytes.Buffer
	buf.Write(helps.WrapConnectEnvelope(f1))
	buf.Write(helps.WrapConnectEnvelopeWithFlag(helps.ConnectFlagEndStream, []byte(`{}`)))

	out := make(chan coreexecutor.StreamChunk, 20)
	opts := coreexecutor.Options{
		SourceFormat: translatorreg.FormatInteractions,
	}

	go func() {
		defer close(out)
		exec.streamDevinFrames(
			ctx,
			&buf,
			coreexecutor.Request{Model: "devin/swe-2"},
			opts,
			"swe-2-high",
			translatorreg.FormatInteractions,
			reporter,
			out,
		)
	}()

	for range out {
	}

	record := capture.await(t)
	if record.Model != "devin/swe-2" {
		t.Fatalf("record model = %q, want devin/swe-2", record.Model)
	}
	if record.ResponseModel != "gpt-5-6-luna-low" {
		t.Fatalf("record response model = %q, want gpt-5-6-luna-low", record.ResponseModel)
	}
}

func TestDevinStreamEmptyWhenNoModelNameInUsage(t *testing.T) {
	const alias = "devin-stream-no-model-test"
	capture := &multiProviderUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
	coreusage.RegisterNamedConsumer(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedConsumer(t.Name(), multiProviderNoopUsageConsumer{})
	})

	ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
	auth := &coreauth.Auth{ID: "devin-auth-2", Index: "auth-devin-2", Provider: "devin"}
	exec := NewDevinExecutor(&config.Config{})
	reporter := helps.NewExecutorUsageReporter(ctx, exec, "devin/swe-2", auth)

	var f7 []byte
	f7 = protowire.AppendTag(f7, 2, protowire.VarintType)
	f7 = protowire.AppendVarint(f7, 10)
	f7 = protowire.AppendTag(f7, 3, protowire.VarintType)
	f7 = protowire.AppendVarint(f7, 20)

	var f1 []byte
	f1 = protowire.AppendTag(f1, 7, protowire.BytesType)
	f1 = protowire.AppendBytes(f1, f7)

	var buf bytes.Buffer
	buf.Write(helps.WrapConnectEnvelope(f1))
	buf.Write(helps.WrapConnectEnvelopeWithFlag(helps.ConnectFlagEndStream, []byte(`{}`)))

	out := make(chan coreexecutor.StreamChunk, 20)
	opts := coreexecutor.Options{
		SourceFormat: translatorreg.FormatInteractions,
	}

	go func() {
		defer close(out)
		exec.streamDevinFrames(
			ctx,
			&buf,
			coreexecutor.Request{Model: "devin/swe-2"},
			opts,
			"swe-2-high",
			translatorreg.FormatInteractions,
			reporter,
			out,
		)
	}()

	for range out {
	}

	record := capture.await(t)
	if record.ResponseModel != "" {
		t.Fatalf("record response model = %q, want empty string when unknown", record.ResponseModel)
	}
}

func TestDevinNonStreamRecordsResponseModelFromUsage(t *testing.T) {
	const alias = "devin-nonstream-response-model-test"
	capture := &multiProviderUsageCapture{alias: alias, records: make(chan coreusage.Record, 4)}
	coreusage.RegisterNamedConsumer(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedConsumer(t.Name(), multiProviderNoopUsageConsumer{})
	})

	ctx := coreusage.WithRequestedModelAlias(context.Background(), alias)
	auth := &coreauth.Auth{ID: "devin-auth-3", Index: "auth-devin-3", Provider: "devin"}
	exec := NewDevinExecutor(&config.Config{})
	reporter := helps.NewExecutorUsageReporter(ctx, exec, "devin/swe-2", auth)

	var f7 []byte
	f7 = protowire.AppendTag(f7, 2, protowire.VarintType)
	f7 = protowire.AppendVarint(f7, 10)
	f7 = protowire.AppendTag(f7, 3, protowire.VarintType)
	f7 = protowire.AppendVarint(f7, 20)
	f7 = protowire.AppendTag(f7, 9, protowire.BytesType)
	f7 = protowire.AppendString(f7, "gpt-5-6-luna-low")

	var f1 []byte
	f1 = protowire.AppendTag(f1, 7, protowire.BytesType)
	f1 = protowire.AppendBytes(f1, f7)

	var buf bytes.Buffer
	buf.Write(helps.WrapConnectEnvelope(f1))
	buf.Write(helps.WrapConnectEnvelopeWithFlag(helps.ConnectFlagEndStream, []byte(`{}`)))

	interactionsJSON, respLog, errConsume := consumeDevinFramesToInteractions(&buf, "devin/swe-2", "chat-model-uid")
	if errConsume != nil {
		t.Fatalf("consumeDevinFramesToInteractions failed: %v", errConsume)
	}

	if respLog != nil && respLog.Usage != nil && respLog.Usage.ModelName != "" {
		reporter.SetResponseModel(respLog.Usage.ModelName)
	}
	reporter.Publish(ctx, helps.ParseInteractionsUsage(interactionsJSON))

	record := capture.await(t)
	if record.ResponseModel != "gpt-5-6-luna-low" {
		t.Fatalf("record response model = %q, want gpt-5-6-luna-low", record.ResponseModel)
	}
}
