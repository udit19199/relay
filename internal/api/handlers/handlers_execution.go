package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/udit19199/relay/internal/clienterror"
	"github.com/udit19199/relay/internal/interfaces"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
	"golang.org/x/net/context"
)

// ExecuteWithAuthManager executes a non-streaming request via the core auth manager.
// This path is the only supported execution route.
func (h *BaseAPIHandler) ExecuteWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage) {
	return h.executeWithAuthManager(ctx, handlerType, modelName, rawJSON, alt)
}

func (h *BaseAPIHandler) executeWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage) {
	return h.executeWithAuthManagerFormats(ctx, handlerType, handlerType, modelName, rawJSON, alt, modelExecutionOptions{})
}

func (h *BaseAPIHandler) executeWithAuthManagerFormats(ctx context.Context, entryProtocol, exitProtocol, modelName string, rawJSON []byte, alt string, execOptions modelExecutionOptions) ([]byte, http.Header, *interfaces.ErrorMessage) {
	originalRequestedModel := modelName
	responseProtocol := modelExecutionResponseProtocol(entryProtocol, exitProtocol)
	providers, normalizedModel, errMsg := h.providersForExecution(modelName, originalRequestedModel, execOptions)
	if errMsg != nil {
		return nil, nil, errMsg
	}
	reqMeta := requestExecutionMetadata(ctx)
	if execOptions.Path != "" {
		reqMeta[coreexecutor.RequestPathMetadataKey] = execOptions.Path
	}
	addCallerScopeMetadata(reqMeta, execOptions.CallerScope)
	reqMeta[coreexecutor.RequestedModelMetadataKey] = originalRequestedModel
	addAuthSelectionModelMetadata(reqMeta, execOptions.AuthSelectionModel)
	setReasoningEffortMetadata(reqMeta, entryProtocol, normalizedModel, rawJSON)
	setServiceTierMetadata(reqMeta, rawJSON)
	setGenerateMetadata(reqMeta, rawJSON)
	payload := rawJSON
	if len(payload) == 0 {
		payload = nil
	}
	req := coreexecutor.Request{
		Model:   normalizedModel,
		Payload: payload,
	}
	originalRequest := rawJSON
	if ctxOriginal := originalRequestFromContext(ctx); len(ctxOriginal) > 0 {
		originalRequest = ctxOriginal
	}
	opts := coreexecutor.Options{
		Stream:          false,
		Alt:             alt,
		OriginalRequest: originalRequest,
		SourceFormat:    translatorreg.FromString(entryProtocol),
		ResponseFormat:  translatorreg.FromString(responseProtocol),
		Headers:         modelExecutionHeaders(ctx, execOptions.Headers),
		Query:           modelExecutionQuery(ctx, execOptions.Query),
		ProxyURL:        execOptions.ProxyURL,
	}
	opts.Metadata = reqMeta
	ctx = enrichContextWithSessionHierarchy(ctx, opts.Headers, req.Payload, opts.Metadata)
	resp, err := h.AuthManager.Execute(ctx, providers, req, opts)
	if err != nil {
		err = enrichAuthSelectionError(err, providers, normalizedModel)
		errMsg := executionErrorMessage(err)
		return nil, nil, errMsg
	}
	ctx = enrichContextWithSessionHierarchy(ctx, opts.Headers, req.Payload, opts.Metadata)
	rawResponseHeaders := cloneHeader(resp.Headers)
	passthroughHeaders := executionPassthroughHeaders(h.CurrentConfig())
	responseHeaders := downstreamHeadersFromExecutor(rawResponseHeaders, passthroughHeaders)
	return resp.Payload, responseHeaders, nil
}

// ExecuteCountWithAuthManager executes a non-streaming request via the core auth manager.
// This path is the only supported execution route.
func (h *BaseAPIHandler) ExecuteCountWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage) {
	return h.executeCountWithAuthManager(ctx, handlerType, modelName, rawJSON, alt, modelExecutionOptions{})
}

func (h *BaseAPIHandler) executeCountWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string, execOptions modelExecutionOptions) ([]byte, http.Header, *interfaces.ErrorMessage) {
	originalRequestedModel := modelName
	providers, normalizedModel, errMsg := h.providersForExecution(modelName, originalRequestedModel, execOptions)
	if errMsg != nil {
		return nil, nil, errMsg
	}
	reqMeta := requestExecutionMetadata(ctx)
	addCallerScopeMetadata(reqMeta, execOptions.CallerScope)
	reqMeta[coreexecutor.RequestedModelMetadataKey] = originalRequestedModel
	addAuthSelectionModelMetadata(reqMeta, execOptions.AuthSelectionModel)
	setReasoningEffortMetadata(reqMeta, handlerType, normalizedModel, rawJSON)
	setServiceTierMetadata(reqMeta, rawJSON)
	setGenerateMetadata(reqMeta, rawJSON)
	payload := rawJSON
	if len(payload) == 0 {
		payload = nil
	}
	req := coreexecutor.Request{
		Model:   normalizedModel,
		Payload: payload,
	}
	opts := coreexecutor.Options{
		Stream:          false,
		Alt:             alt,
		OriginalRequest: rawJSON,
		SourceFormat:    translatorreg.FromString(handlerType),
		Headers:         modelExecutionHeaders(ctx, execOptions.Headers),
		Query:           modelExecutionQuery(ctx, execOptions.Query),
		ProxyURL:        execOptions.ProxyURL,
	}
	opts.Metadata = reqMeta
	ctx = enrichContextWithSessionHierarchy(ctx, opts.Headers, req.Payload, opts.Metadata)
	resp, err := h.AuthManager.ExecuteCount(ctx, providers, req, opts)
	if err != nil {
		err = enrichAuthSelectionError(err, providers, normalizedModel)
		errMsg := executionErrorMessage(err)
		return nil, nil, errMsg
	}
	ctx = enrichContextWithSessionHierarchy(ctx, opts.Headers, req.Payload, opts.Metadata)
	rawResponseHeaders := cloneHeader(resp.Headers)
	passthroughHeaders := executionPassthroughHeaders(h.CurrentConfig())
	responseHeaders := downstreamHeadersFromExecutor(rawResponseHeaders, passthroughHeaders)
	return resp.Payload, responseHeaders, nil
}

func normalizedTerminationStatus(statusCode int) int {
	if statusCode < http.StatusOK || statusCode > 599 {
		return http.StatusForbidden
	}
	return statusCode
}

func ExecutionErrorMessage(err error) *interfaces.ErrorMessage {
	return executionErrorMessage(err)
}

func executionErrorMessage(err error) *interfaces.ErrorMessage {
	var terminated *coreexecutor.RequestTerminatedError
	if errors.As(err, &terminated) && terminated != nil {
		return &interfaces.ErrorMessage{
			StatusCode:     normalizedTerminationStatus(terminated.StatusCode()),
			Error:          err,
			DirectResponse: true,
			Body:           terminated.ResponseBody(),
			Headers:        terminated.ResponseHeaders(),
		}
	}
	status := http.StatusInternalServerError
	if code := clienterror.HTTPStatusFromError(err); code > 0 {
		status = code
	}
	type directResponseError interface {
		DirectResponse() bool
		ResponseBody() []byte
	}
	var direct directResponseError
	if errors.As(err, &direct) && direct != nil && direct.DirectResponse() {
		body := direct.ResponseBody()
		var headers http.Header
		if len(body) > 0 {
			contentType := http.DetectContentType(body)
			if json.Valid(body) {
				contentType = "application/json"
			}
			headers = http.Header{"Content-Type": []string{contentType}}
		}
		return &interfaces.ErrorMessage{
			StatusCode:     status,
			Error:          err,
			DirectResponse: true,
			Body:           body,
			Headers:        headers,
		}
	}
	var addon http.Header
	if he, ok := err.(interface{ Headers() http.Header }); ok && he != nil {
		if hdr := he.Headers(); hdr != nil {
			addon = hdr.Clone()
		}
	}
	return &interfaces.ErrorMessage{StatusCode: status, Error: err, Addon: addon}
}
