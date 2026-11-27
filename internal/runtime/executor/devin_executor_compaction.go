package executor

import (
	"context"
	"net/http"

	"github.com/udit19199/relay/internal/runtime/executor/helps"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	"github.com/udit19199/relay/internal/thinking"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

// executeDevinResponsesCompaction handles Codex remote compaction v2 for the
// native Devin route. Devin has no native compaction endpoint, so the summary
// is generated with the same model through the normal Execute path and sealed
// into a CPA compaction capsule, mirroring the Meta/OpenAI-compat routes.
func (e *DevinExecutor) executeDevinResponsesCompaction(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	var resp coreexecutor.Response
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	summaryPayload := helps.PrepareResponsesCompactionSummaryPayload(helps.ResponsesCompactionSourcePayload(req, opts), baseModel)
	summaryReq := coreexecutor.Request{
		Model:    req.Model,
		Payload:  summaryPayload,
		Metadata: req.Metadata,
	}
	summaryOpts := opts
	summaryOpts.Alt = ""
	summaryOpts.Stream = false
	summaryOpts.OriginalRequest = nil
	summaryOpts.SourceFormat = translatorreg.FormatOpenAIResponse
	summaryOpts.ResponseFormat = translatorreg.FormatOpenAIResponse

	summaryResp, errSummary := e.Execute(ctx, auth, summaryReq, summaryOpts)
	if errSummary != nil {
		return resp, errSummary
	}
	respPayload, errBuild := helps.BuildResponsesCompactionResponseFromSummary(baseModel, summaryResp.Payload)
	if errBuild != nil {
		return resp, statusErr{code: http.StatusBadGateway, msg: errBuild.Error()}
	}
	return coreexecutor.Response{
		Payload: respPayload,
		Headers: summaryResp.Headers,
	}, nil
}

// executeDevinResponsesCompactionStream emits the sealed compaction response
// as the Responses SSE event sequence Codex expects for remote compaction v2.
func (e *DevinExecutor) executeDevinResponsesCompactionStream(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	summaryResp, errSummary := e.executeDevinResponsesCompaction(ctx, auth, req, opts)
	if errSummary != nil {
		return nil, errSummary
	}
	chunks, errChunks := helps.BuildResponsesCompactionStreamChunksFromResponse(baseModel, summaryResp.Payload)
	if errChunks != nil {
		return nil, statusErr{code: http.StatusBadGateway, msg: errChunks.Error()}
	}
	out := make(chan coreexecutor.StreamChunk, len(chunks))
	for _, chunk := range chunks {
		out <- coreexecutor.StreamChunk{Payload: chunk}
	}
	close(out)

	headers := summaryResp.Headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Type", "text/event-stream")
	return &coreexecutor.StreamResult{Headers: headers, Chunks: out}, nil
}
