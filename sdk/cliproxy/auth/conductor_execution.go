package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	cliproxysession "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/session"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
)

func newUpstreamAttemptContext(ctx context.Context) context.Context {
	ctx = logging.WithFreshResponseHeadersHolder(ctx)
	return cliproxyexecutor.WithUpstreamAttemptTracker(ctx)
}

func claudeOAuthRequestCancellation(ctx context.Context, auth *Auth, err error) error {
	if !authProviderIs(auth, "claude") || !strings.EqualFold(strings.TrimSpace(auth.Attributes["auth_kind"]), "oauth") {
		return nil
	}
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

type upstreamExecutionAttemptError struct {
	cause error
}

func (e *upstreamExecutionAttemptError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *upstreamExecutionAttemptError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func markUpstreamExecutionAttempt(err error) error {
	if err == nil {
		return nil
	}
	if hasUpstreamExecutionAttempt(err) {
		return err
	}
	return &upstreamExecutionAttemptError{cause: err}
}

func markUpstreamExecutionAttemptFromContext(ctx context.Context, err error) error {
	if err == nil || !cliproxyexecutor.UpstreamAttempted(ctx) {
		return err
	}
	return markUpstreamExecutionAttempt(err)
}

func hasUpstreamExecutionAttempt(err error) bool {
	var marked *upstreamExecutionAttemptError
	return errors.As(err, &marked) && marked != nil
}

func unwrapUpstreamExecutionAttempt(err error) error {
	marked, ok := err.(*upstreamExecutionAttemptError)
	if !ok || marked == nil || marked.cause == nil {
		return err
	}
	return marked.cause
}

func unwrapExecutionBoundaryError(err error) error {
	err = unwrapRequestStopError(err)
	return unwrapUpstreamExecutionAttempt(err)
}

func preferredExecutionAttemptError(fallback, upstream error) error {
	if errors.Is(fallback, context.Canceled) || errors.Is(fallback, context.DeadlineExceeded) || isSessionAffinityRetryError(fallback) {
		return fallback
	}
	if upstream == nil {
		return fallback
	}
	return markUpstreamExecutionAttempt(upstream)
}

func (m *Manager) normalizeSessionAffinityNeverTerminalError(err error, providers []string, model string, opts cliproxyexecutor.Options) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if isRequestTerminatedError(err) || isRequestStopError(err) || isRequestInvalidError(err) || isSessionAffinityRetryError(err) {
		return err
	}
	var authErr *Error
	if errors.As(err, &authErr) && authErr != nil {
		switch authErr.Code {
		case "session_auth_unavailable", "auth_not_found", "auth_unavailable", "model_cooldown", "provider_not_found", "executor_not_found":
			return err
		}
	}
	var cooldownErr *modelCooldownError
	if errors.As(err, &cooldownErr) && cooldownErr != nil {
		return err
	}
	affinity, ok := m.Selector().(*SessionAffinitySelector)
	if !ok || affinity == nil || !affinity.rebindOnNever {
		return err
	}
	if _, okBound := affinity.boundAuthForRequest(providers, model, opts); !okBound {
		return err
	}
	rerr := resultErrorFromError(err)
	if shouldRebindSessionAffinityOnError(rerr) {
		return err
	}
	if shouldSkipCredentialCooldown(rerr) && !isTransientTransportResultError(rerr) && !isConnectionLifecycleResultError(rerr) {
		return err
	}
	return &Error{
		Code:       "session_auth_retry",
		Message:    "bound session credential is transiently unavailable",
		Retryable:  true,
		HTTPStatus: http.StatusServiceUnavailable,
	}
}

// Execute performs a non-streaming execution using the configured selector and executor.
// It supports multiple providers for the same model and round-robins the starting provider per model.
func isOpenAIImageExecutionOptions(opts cliproxyexecutor.Options) bool {
	return strings.EqualFold(strings.TrimSpace(opts.SourceFormat.String()), "openai-image")
}

func (m *Manager) Execute(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	ctx = cliproxyexecutor.WithRequestProxyURL(ctx, opts.ProxyURL)
	req, opts = cliproxysession.Enrich(req, opts)
	ctx = WithCodexClientRequest(ctx, isOpenAIImageExecutionOptions(opts) || IsCodexClientRequest(ctx, opts.Headers))
	ctx = coreusage.CaptureIdentity(ctx)
	normalized := m.normalizeProviders(providers)
	if len(normalized) == 0 {
		return cliproxyexecutor.Response{}, &Error{Code: "provider_not_found", Message: "no provider supplied"}
	}

	defaultRequestRetry, maxRetryCredentials, maxWait := m.retrySettings()

	var lastErr error
	var preferredUpstreamErr error
	retryModel := authSelectionModelFromOptions(opts, req.Model)
	for attempt := 0; ; attempt++ {
		roundAttempted := make(map[string]struct{})
		roundOpts := withAttemptedAuthTracker(opts, roundAttempted)
		resp, errExec := m.executeMixedOnce(ctx, normalized, req, roundOpts, maxRetryCredentials, attempt, defaultRequestRetry)
		if errExec == nil {
			return resp, nil
		}
		if isRequestTerminatedError(errExec) || isRequestStopError(errExec) {
			return cliproxyexecutor.Response{}, unwrapExecutionBoundaryError(errExec)
		}
		if hasUpstreamExecutionAttempt(errExec) {
			preferredUpstreamErr = errExec
		}
		lastErr = errExec
		wait, shouldRetry := m.shouldRetryAfterErrorWithAttempted(ctx, opts, errExec, attempt, normalized, retryModel, maxWait, defaultRequestRetry, roundAttempted)
		if !shouldRetry {
			break
		}
		if errWait := waitForCooldown(ctx, wait, maxWait); errWait != nil {
			return cliproxyexecutor.Response{}, errWait
		}
	}
	if lastErr != nil {
		if ctx != nil {
			if errCtx := ctx.Err(); errCtx != nil {
				return cliproxyexecutor.Response{}, errCtx
			}
		}
		lastErr = preferredExecutionAttemptError(lastErr, preferredUpstreamErr)
		lastErr = unwrapExecutionBoundaryError(lastErr)
		lastErr = m.normalizeSessionAffinityNeverTerminalError(lastErr, normalized, retryModel, opts)
		return cliproxyexecutor.Response{}, lastErr
	}
	return cliproxyexecutor.Response{}, &Error{Code: "auth_not_found", Message: "no auth available"}
}

// It supports multiple providers for the same model and round-robins the starting provider per model.
func (m *Manager) ExecuteCount(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	ctx = cliproxyexecutor.WithRequestProxyURL(ctx, opts.ProxyURL)
	req, opts = cliproxysession.Enrich(req, opts)
	ctx = WithCodexClientRequest(ctx, IsCodexClientRequest(ctx, opts.Headers))
	ctx = coreusage.CaptureIdentity(ctx)
	normalized := m.normalizeProviders(providers)
	if len(normalized) == 0 {
		return cliproxyexecutor.Response{}, &Error{Code: "provider_not_found", Message: "no provider supplied"}
	}

	defaultRequestRetry, maxRetryCredentials, maxWait := m.retrySettings()

	var lastErr error
	var preferredUpstreamErr error
	retryModel := authSelectionModelFromOptions(opts, req.Model)
	for attempt := 0; ; attempt++ {
		roundAttempted := make(map[string]struct{})
		roundOpts := withAttemptedAuthTracker(opts, roundAttempted)
		resp, errExec := m.executeCountMixedOnce(ctx, normalized, req, roundOpts, maxRetryCredentials, attempt, defaultRequestRetry)
		if errExec == nil {
			return resp, nil
		}
		if isRequestTerminatedError(errExec) || isRequestStopError(errExec) {
			return cliproxyexecutor.Response{}, unwrapExecutionBoundaryError(errExec)
		}
		if hasUpstreamExecutionAttempt(errExec) {
			preferredUpstreamErr = errExec
		}
		lastErr = errExec
		wait, shouldRetry := m.shouldRetryAfterErrorWithAttempted(ctx, opts, errExec, attempt, normalized, retryModel, maxWait, defaultRequestRetry, roundAttempted)
		if !shouldRetry {
			break
		}
		if errWait := waitForCooldown(ctx, wait, maxWait); errWait != nil {
			return cliproxyexecutor.Response{}, errWait
		}
	}
	if lastErr != nil {
		if ctx != nil {
			if errCtx := ctx.Err(); errCtx != nil {
				return cliproxyexecutor.Response{}, errCtx
			}
		}
		lastErr = preferredExecutionAttemptError(lastErr, preferredUpstreamErr)
		lastErr = unwrapExecutionBoundaryError(lastErr)
		lastErr = m.normalizeSessionAffinityNeverTerminalError(lastErr, normalized, retryModel, opts)
		return cliproxyexecutor.Response{}, lastErr
	}
	return cliproxyexecutor.Response{}, &Error{Code: "auth_not_found", Message: "no auth available"}
}

// ExecuteStream performs a streaming execution using the configured selector and executor.
// It supports multiple providers for the same model and round-robins the starting provider per model.
func (m *Manager) ExecuteStream(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	ctx = cliproxyexecutor.WithRequestProxyURL(ctx, opts.ProxyURL)
	req, opts = cliproxysession.Enrich(req, opts)
	ctx = WithCodexClientRequest(ctx, isOpenAIImageExecutionOptions(opts) || IsCodexClientRequest(ctx, opts.Headers))
	ctx = coreusage.CaptureIdentity(ctx)
	normalized := m.normalizeProviders(providers)
	if len(normalized) == 0 {
		return nil, &Error{Code: "provider_not_found", Message: "no provider supplied"}
	}

	defaultRequestRetry, maxRetryCredentials, maxWait := m.retrySettings()

	var lastErr error
	var preferredUpstreamErr error
	retryModel := authSelectionModelFromOptions(opts, req.Model)
	attempt := 0
	for {
		roundAttempted := make(map[string]struct{})
		roundOpts := withAttemptedAuthTracker(opts, roundAttempted)
		result, errStream := m.executeStreamMixedOnce(ctx, normalized, req, roundOpts, maxRetryCredentials, attempt, defaultRequestRetry)
		if errStream == nil {
			return result, nil
		}
		if hasUpstreamExecutionAttempt(errStream) {
			preferredUpstreamErr = errStream
		}
		if isRequestTerminatedError(errStream) || isRequestStopError(errStream) {
			return nil, unwrapExecutionBoundaryError(errStream)
		}
		lastErr = errStream
		wait, shouldRetry := m.shouldRetryAfterErrorWithAttempted(ctx, opts, errStream, attempt, normalized, retryModel, maxWait, defaultRequestRetry, roundAttempted)
		if !shouldRetry {
			break
		}
		if errWait := waitForCooldown(ctx, wait, maxWait); errWait != nil {
			return nil, errWait
		}
		attempt++
	}
	if lastErr != nil {
		if ctx != nil {
			if errCtx := ctx.Err(); errCtx != nil {
				return nil, errCtx
			}
		}
		lastErr = preferredExecutionAttemptError(lastErr, preferredUpstreamErr)
		lastErr = unwrapExecutionBoundaryError(lastErr)
		lastErr = m.normalizeSessionAffinityNeverTerminalError(lastErr, normalized, retryModel, opts)
		var bootstrapErr *streamBootstrapError
		if errors.As(lastErr, &bootstrapErr) && bootstrapErr != nil {
			return streamErrorResult(bootstrapErr.Headers(), lastErr), nil
		}
		return nil, lastErr
	}
	return nil, &Error{Code: "auth_not_found", Message: "no auth available"}
}

type requestToFormatResolver interface {
	RequestToFormat(req cliproxyexecutor.Request, opts cliproxyexecutor.Options) sdktranslator.Format
}

func isRequestTerminatedError(err error) bool {
	var terminated *cliproxyexecutor.RequestTerminatedError
	return (errors.As(err, &terminated) && terminated != nil) || cliproxyexecutor.IsUpstreamWebsocketTurnError(err)
}

func applyRequestAfterAuthInterceptor(ctx context.Context, executor ProviderExecutor, provider string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, requestedModel string) (cliproxyexecutor.Request, cliproxyexecutor.Options, error) {
	if opts.RequestAfterAuthInterceptor == nil {
		return req, opts, nil
	}
	toFormat := requestToFormat(provider, executor, req, opts)
	resp := opts.RequestAfterAuthInterceptor(ctx, cliproxyexecutor.RequestAfterAuthInterceptRequest{
		SourceFormat:   opts.SourceFormat,
		ToFormat:       toFormat,
		Model:          req.Model,
		RequestedModel: requestedModel,
		Stream:         opts.Stream,
		Headers:        cloneRequestHeaders(opts.Headers),
		Body:           req.Payload,
		Metadata:       opts.Metadata,
	})
	opts.Headers = mergeRequestHeaders(opts.Headers, resp.Headers, resp.ClearHeaders)
	if len(resp.Body) > 0 {
		req.Payload = resp.Body
		opts.OriginalRequest = resp.Body
	}
	if path := strings.TrimSpace(resp.Path); path != "" {
		if opts.Metadata == nil {
			opts.Metadata = make(map[string]any, 1)
		} else {
			opts.Metadata = maps.Clone(opts.Metadata)
		}
		opts.Metadata[cliproxyexecutor.RequestPathMetadataKey] = path
	}
	if resp.Terminate {
		return req, opts, &cliproxyexecutor.RequestTerminatedError{
			HTTPStatus: resp.StatusCode,
			Header:     cloneRequestHeaders(resp.ResponseHeaders),
			Body:       bytes.Clone(resp.ResponseBody),
		}
	}
	if len(resp.ClearHeaders) > 0 || len(resp.Body) > 0 {
		evalPayload := opts.OriginalRequest
		if len(evalPayload) == 0 {
			evalPayload = req.Payload
		}
		info, ok := cliproxysession.ExtractSessionInfo(opts.Headers, evalPayload, opts.Metadata)
		if ok && info.SessionID != "" {
			if opts.Metadata == nil {
				opts.Metadata = make(map[string]any, 2)
			}
			opts.Metadata[cliproxyexecutor.CanonicalSessionIDMetadataKey] = cliproxysession.BoundSessionIdentity(info.SessionID)
			if info.ParentSessionID != "" && info.ParentSessionID != info.SessionID {
				opts.Metadata[cliproxyexecutor.ParentSessionIDMetadataKey] = cliproxysession.BoundSessionIdentity(info.ParentSessionID)
			} else {
				delete(opts.Metadata, cliproxyexecutor.ParentSessionIDMetadataKey)
			}
		} else {
			delete(opts.Metadata, cliproxyexecutor.CanonicalSessionIDMetadataKey)
			delete(opts.Metadata, cliproxyexecutor.ParentSessionIDMetadataKey)
			delete(opts.Metadata, cliproxyexecutor.LCPAffinitySessionIDMetadataKey)
		}
	} else if len(resp.Headers) > 0 {
		if info, ok := cliproxysession.ExtractSessionInfo(opts.Headers, nil, opts.Metadata); ok && info.SessionID != "" {
			if opts.Metadata == nil {
				opts.Metadata = make(map[string]any, 2)
			}
			opts.Metadata[cliproxyexecutor.CanonicalSessionIDMetadataKey] = cliproxysession.BoundSessionIdentity(info.SessionID)
			if info.ParentSessionID != "" && info.ParentSessionID != info.SessionID {
				opts.Metadata[cliproxyexecutor.ParentSessionIDMetadataKey] = cliproxysession.BoundSessionIdentity(info.ParentSessionID)
			}
		}
	}
	return req, opts, nil
}

func requestToFormat(provider string, executor ProviderExecutor, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) sdktranslator.Format {
	resolver, ok := executor.(requestToFormatResolver)
	if ok && resolver != nil {
		formatRequestTo := resolver.RequestToFormat(req, opts)
		if formatRequestTo != "" {
			return formatRequestTo
		}
	}
	source := opts.SourceFormat.String()
	if source == "openai-image" || source == "openai-video" || source == "openai-speech" {
		return opts.SourceFormat
	}
	if opts.Alt == "responses/compact" && !opts.Stream {
		return sdktranslator.FormatOpenAIResponse
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return sdktranslator.FormatCodex
	case "xai":
		return sdktranslator.FormatCodex
	case "claude":
		return sdktranslator.FormatClaude
	case "meta":
		return sdktranslator.FormatCodex
	case "devin":
		return sdktranslator.FormatInteractions
	default:
		return sdktranslator.FormatOpenAI
	}
}

func cloneRequestHeaders(src http.Header) http.Header {
	if src == nil {
		return nil
	}
	dst := make(http.Header, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func mergeRequestHeaders(current, updates http.Header, clear []string) http.Header {
	if updates == nil && len(clear) == 0 {
		return current
	}
	out := cloneRequestHeaders(current)
	if out == nil && (len(updates) > 0 || len(clear) > 0) {
		out = make(http.Header)
	}
	for _, key := range clear {
		out.Del(key)
		for existingKey := range out {
			if strings.EqualFold(existingKey, key) {
				delete(out, existingKey)
			}
		}
	}
	for key, values := range updates {
		out.Del(key)
		for _, value := range values {
			out.Add(key, value)
		}
	}
	return out
}

func executorForAuth(executor ProviderExecutor, auth *Auth) ProviderExecutor {
	if auth.AuthKind() == AuthKindAPIKey {
		if scoped, ok := executor.(APIKeyConfigExecutor); ok {
			return scoped.ForAPIKey()
		}
	}
	return executor
}

// syncPreferredUpstreamError refreshes the error that terminal selection
// will prefer over the last local failure when the attempt error gained a
// trusted retry annotation after its result was recorded.
func syncPreferredUpstreamError(upstreamErr *error, errExec error) {
	if upstreamErr == nil || errExec == nil || !hasUpstreamExecutionAttempt(errExec) {
		return
	}
	// A stream bootstrap wrapper built from an earlier attempt already
	// carries the response headers needed to hand that failure back as an
	// error stream; keep that earlier failure preferred. When the wrapper
	// was built from this same attempt, annotate that wrapper in place
	// instead of replacing it (error values are compared by identity only
	// through reflect here because dynamic types may be uncomparable).
	var bootstrap *streamBootstrapError
	if errors.As(*upstreamErr, &bootstrap) && bootstrap != nil {
		if hint := trustedRetryAfterHint(errExec); hint != nil && *hint > 0 {
			bootstrap.cause = withTrustedRetryAfter(bootstrap.cause, hint)
		}
		return
	}
	*upstreamErr = errExec
}

func (m *Manager) executeMixedOnce(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, maxRetryCredentials int, retryRound int, defaultRequestRetry int) (cliproxyexecutor.Response, error) {
	if len(providers) == 0 {
		return cliproxyexecutor.Response{}, &Error{Code: "provider_not_found", Message: "no provider supplied"}
	}
	routeModel := authSelectionModelFromOptions(opts, req.Model)
	executionModel, restoreExecutionModel := executionModelForAuthSelection(opts, req.Model)
	opts = ensureRequestedModelMetadata(opts, routeModel)
	tried := make(map[string]struct{})

	for authID := range m.requestRetryRoundExclusions(retryRound, defaultRequestRetry) {
		tried[authID] = struct{}{}
	}
	attempted := make(map[string]struct{})
	var lastErr error
	var upstreamErr error
	for {
		if maxRetryCredentials > 0 && len(attempted) >= maxRetryCredentials {
			if lastErr != nil {
				return cliproxyexecutor.Response{}, preferredExecutionAttemptError(lastErr, upstreamErr)
			}
			return cliproxyexecutor.Response{}, &Error{Code: "auth_not_found", Message: "no auth available"}
		}
		opts.Metadata = cloneRequestMetadata(opts.Metadata)
		pickOpts := opts
		auth, executor, provider, errPick := m.pickNextMixed(ctx, providers, routeModel, pickOpts, tried)
		if errPick != nil {
			if shouldReturnLastErrorOnPickFailure(lastErr, errPick) {
				return cliproxyexecutor.Response{}, preferredExecutionAttemptError(lastErr, upstreamErr)
			}
			return cliproxyexecutor.Response{}, errPick
		}

		entry := logEntryWithRequestID(ctx)
		debugLogAuthSelection(entry, auth, provider, routeModel)
		publishSelectedAuthMetadata(opts.Metadata, auth)

		tried[auth.ID] = struct{}{}
		execCtx := ctx
		if rt := m.roundTripperFor(auth); rt != nil {
			execCtx = context.WithValue(execCtx, roundTripperContextKey{}, rt)
			execCtx = context.WithValue(execCtx, "cliproxy.roundtripper", rt)
		}
		execCtx = contextWithRequestedModelAlias(execCtx, pickOpts, routeModel)
		execCtx = coreusage.WithSelectionID(execCtx, selectionIDFromOptions(pickOpts))
		execCtx = newUpstreamAttemptContext(execCtx)

		models, pooled, aliasResult, routing := m.preparedExecutionModelsWithAliasForRequest(execCtx, auth, routeModel, pickOpts.Headers)
		if len(models) == 0 {
			continue
		}
		attempted[auth.ID] = struct{}{}
		var errPrepare error
		auth, errPrepare = m.prepareRequestAuth(execCtx, executor, auth)
		if errPrepare != nil {
			if errCancel := claudeOAuthRequestCancellation(execCtx, auth, errPrepare); errCancel != nil {
				return cliproxyexecutor.Response{}, errCancel
			}
			stateModel := m.selectionModelKeyForAuth(auth, routeModel)
			if stateModel == "" {
				stateModel = canonicalModelKey(routeModel)
			}
			result := Result{AuthID: auth.ID, Provider: provider, Model: stateModel, RouteModel: routeModel, Success: false, Error: resultErrorFromError(errPrepare), Options: pickOpts, AccessToken: authAccessTokenForRequest(execCtx, auth, pickOpts.Headers), CredentialVersion: auth.CredentialVersion, RegistrationEpoch: auth.RegistrationEpoch}
			m.MarkResult(execCtx, result)
			lastErr = errPrepare
			continue
		}
		executor = executorForAuth(executor, auth)
		var authErr error
		didRefreshOnUnauthorized := false
		for _, upstreamModel := range models {
			execCtx = newUpstreamAttemptContext(execCtx)
			resultModel := m.stateModelForExecution(auth, routeModel, upstreamModel, pooled)
			execReq := req
			execReq.Model = upstreamModel
			if restoreExecutionModel {
				execReq.Model = executionModel
			}
			execOpts := opts
			if pickOpts.Metadata != nil {
				if canonicalID, ok := pickOpts.Metadata[cliproxyexecutor.CanonicalSessionIDMetadataKey]; ok {
					meta := make(map[string]any, len(execOpts.Metadata)+2)
					for k, v := range execOpts.Metadata {
						meta[k] = v
					}
					meta[cliproxyexecutor.CanonicalSessionIDMetadataKey] = canonicalID
					if parentID, okParent := pickOpts.Metadata[cliproxyexecutor.ParentSessionIDMetadataKey]; okParent && parentID != canonicalID {
						meta[cliproxyexecutor.ParentSessionIDMetadataKey] = parentID
					} else {
						delete(meta, cliproxyexecutor.ParentSessionIDMetadataKey)
					}
					execOpts.Metadata = meta
				}
			}
			payload := execOpts.OriginalRequest
			if len(payload) == 0 {
				payload = execReq.Payload
			}
			execOpts.Metadata = ensureCanonicalSessionMetadata(execOpts.Metadata, execOpts.Headers, payload)
			var errIntercept error
			execReq, execOpts, errIntercept = applyRequestAfterAuthInterceptor(execCtx, executor, provider, execReq, execOpts, requestedModelAliasFromOptions(execOpts, routeModel))
			if errIntercept != nil {
				return cliproxyexecutor.Response{}, errIntercept
			}
			execReq = attachResolvedExecutionModelInfo(routing, execReq, auth, routeModel, upstreamModel, restoreExecutionModel)
			execCtx = syncMetadataSessionToContext(execCtx, execOpts.Metadata)
			executionSeq := m.nextExecutionSeq()
			startExec := time.Now()
			resp, errExec := executor.Execute(execCtx, auth, execReq, execOpts)
			errExec = markUpstreamExecutionAttemptFromContext(execCtx, errExec)
			durationExec := time.Since(startExec)
			if errExec != nil {
				if hasUpstreamExecutionAttempt(errExec) {
					upstreamErr = errExec
				}
				if errCtx := execCtx.Err(); errCtx != nil {
					return cliproxyexecutor.Response{}, errCtx
				}
				refreshCtx := newUpstreamAttemptContext(execCtx)
				if refreshed, okRefresh := m.tryRefreshAfterUnauthorized(refreshCtx, auth, errExec, didRefreshOnUnauthorized, execOpts.Headers); okRefresh {
					auth = refreshed
					didRefreshOnUnauthorized = true
					execCtx = newUpstreamAttemptContext(execCtx)
					execCtx = syncMetadataSessionToContext(execCtx, execOpts.Metadata)
					executionSeq = m.nextExecutionSeq()
					startRetry := time.Now()
					resp, errExec = executor.Execute(execCtx, auth, execReq, execOpts)
					errExec = markUpstreamExecutionAttemptFromContext(execCtx, errExec)
					durationRetry := time.Since(startRetry)
					if errExec != nil {
						if hasUpstreamExecutionAttempt(errExec) {
							upstreamErr = errExec
						}
						warnLogUpstreamFailure(execCtx, entry, provider, upstreamModel, auth, durationRetry, errExec)
						if errCtx := execCtx.Err(); errCtx != nil {
							return cliproxyexecutor.Response{}, errCtx
						}
					}
				} else {
					warnLogUpstreamFailure(execCtx, entry, provider, upstreamModel, auth, durationExec, errExec)
				}
			}
			if errCancel := claudeOAuthRequestCancellation(execCtx, auth, errExec); errCancel != nil {
				return cliproxyexecutor.Response{}, errCancel
			}
			result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: errExec == nil, CompletedResponse: errExec == nil && !resp.Incomplete && isCompletedExecutionPayload(resp.Payload), Options: execOpts, AccessToken: authAccessTokenForRequest(execCtx, auth, execOpts.Headers), CredentialVersion: auth.CredentialVersion, RegistrationEpoch: auth.RegistrationEpoch, ExecutionSeq: executionSeq}
			if errExec != nil {
				result.Error = resultErrorFromError(errExec)
				if ra := retryAfterFromError(errExec); ra != nil {
					result.RetryAfter = ra
				}
				if isCredentialScopedError(errExec) {
					result.CredentialScope = true
				}
				action, okAction := matchRequestScopedErrorAction(auth, errExec, m.runtimeConfigSnapshot())
				applyRequestScopedActionToResult(action, okAction, &result)
				if isResponsesCompactAvailabilityNeutralError(execOpts, errExec, result.Error) {
					m.recordAvailabilityNeutralResult(execCtx, result)
				} else {
					m.MarkResult(execCtx, result)
					errExec = annotateTrustedRetryAfterByHint(errExec)
					syncPreferredUpstreamError(&upstreamErr, errExec)
				}
				if okAction {
					if isRequestScopedStop(action, okAction) {
						return cliproxyexecutor.Response{}, wrapRequestStopError(errExec)
					}
					authErr = errExec
					if result.CredentialScope {
						break
					}
					continue
				}
				if isResponsesCompactRequestFaultError(execOpts, errExec) || isRequestInvalidError(errExec) {
					return cliproxyexecutor.Response{}, errExec
				}
				authErr = errExec
				if result.CredentialScope {
					break
				}
				continue
			}
			m.MarkResult(execCtx, result)
			attemptAliasResult := resolveAttemptAliasResult(routing, auth, routeModel, upstreamModel, aliasResult)
			rewriteForceMappedResponse(&resp, attemptAliasResult)
			return resp, nil
		}
		if authErr != nil {
			action, okAction := matchRequestScopedErrorAction(auth, authErr, m.runtimeConfigSnapshot())
			if okAction {
				if isRequestScopedStop(action, okAction) {
					return cliproxyexecutor.Response{}, wrapRequestStopError(authErr)
				}
				lastErr = authErr
				continue
			}
			if isResponsesCompactRequestFaultError(opts, authErr) || isRequestInvalidError(authErr) {
				return cliproxyexecutor.Response{}, authErr
			}
			lastErr = authErr
			continue
		}
	}
}

func (m *Manager) executeCountMixedOnce(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, maxRetryCredentials int, retryRound int, defaultRequestRetry int) (cliproxyexecutor.Response, error) {
	if len(providers) == 0 {
		return cliproxyexecutor.Response{}, &Error{Code: "provider_not_found", Message: "no provider supplied"}
	}
	routeModel := authSelectionModelFromOptions(opts, req.Model)
	executionModel, restoreExecutionModel := executionModelForAuthSelection(opts, req.Model)
	opts = ensureRequestedModelMetadata(opts, routeModel)
	tried := make(map[string]struct{})

	for authID := range m.requestRetryRoundExclusions(retryRound, defaultRequestRetry) {
		tried[authID] = struct{}{}
	}
	attempted := make(map[string]struct{})
	var lastErr error
	var upstreamErr error
	for {
		if maxRetryCredentials > 0 && len(attempted) >= maxRetryCredentials {
			if lastErr != nil {
				return cliproxyexecutor.Response{}, preferredExecutionAttemptError(lastErr, upstreamErr)
			}
			return cliproxyexecutor.Response{}, &Error{Code: "auth_not_found", Message: "no auth available"}
		}
		opts.Metadata = cloneRequestMetadata(opts.Metadata)
		pickOpts := opts
		auth, executor, provider, errPick := m.pickNextMixed(ctx, providers, routeModel, pickOpts, tried)
		if errPick != nil {
			if shouldReturnLastErrorOnPickFailure(lastErr, errPick) {
				return cliproxyexecutor.Response{}, preferredExecutionAttemptError(lastErr, upstreamErr)
			}
			return cliproxyexecutor.Response{}, errPick
		}

		entry := logEntryWithRequestID(ctx)
		debugLogAuthSelection(entry, auth, provider, routeModel)
		publishSelectedAuthMetadata(opts.Metadata, auth)

		tried[auth.ID] = struct{}{}
		execCtx := ctx
		if rt := m.roundTripperFor(auth); rt != nil {
			execCtx = context.WithValue(execCtx, roundTripperContextKey{}, rt)
			execCtx = context.WithValue(execCtx, "cliproxy.roundtripper", rt)
		}
		execCtx = contextWithRequestedModelAlias(execCtx, pickOpts, routeModel)
		execCtx = coreusage.WithSelectionID(execCtx, selectionIDFromOptions(pickOpts))
		execCtx = newUpstreamAttemptContext(execCtx)

		models, pooled, aliasResult, routing := m.preparedExecutionModelsWithAliasForRequest(execCtx, auth, routeModel, pickOpts.Headers)
		if len(models) == 0 {
			continue
		}
		attempted[auth.ID] = struct{}{}
		var errPrepare error
		auth, errPrepare = m.prepareRequestAuth(execCtx, executor, auth)
		if errPrepare != nil {
			if errCancel := claudeOAuthRequestCancellation(execCtx, auth, errPrepare); errCancel != nil {
				return cliproxyexecutor.Response{}, errCancel
			}
			stateModel := m.selectionModelKeyForAuth(auth, routeModel)
			if stateModel == "" {
				stateModel = canonicalModelKey(routeModel)
			}
			result := Result{AuthID: auth.ID, Provider: provider, Model: stateModel, RouteModel: routeModel, Success: false, Error: resultErrorFromError(errPrepare), Options: pickOpts, SkipQuotaObservation: true, AccessToken: authAccessTokenForRequest(execCtx, auth, pickOpts.Headers), CredentialVersion: auth.CredentialVersion, RegistrationEpoch: auth.RegistrationEpoch}
			m.MarkResult(execCtx, result)
			lastErr = errPrepare
			continue
		}
		executor = executorForAuth(executor, auth)
		var authErr error
		didRefreshOnUnauthorized := false
		for _, upstreamModel := range models {
			execCtx = newUpstreamAttemptContext(execCtx)
			resultModel := m.stateModelForExecution(auth, routeModel, upstreamModel, pooled)
			execReq := req
			execReq.Model = upstreamModel
			if restoreExecutionModel {
				execReq.Model = executionModel
			}
			execOpts := opts
			if pickOpts.Metadata != nil {
				if canonicalID, ok := pickOpts.Metadata[cliproxyexecutor.CanonicalSessionIDMetadataKey]; ok {
					meta := make(map[string]any, len(execOpts.Metadata)+2)
					for k, v := range execOpts.Metadata {
						meta[k] = v
					}
					meta[cliproxyexecutor.CanonicalSessionIDMetadataKey] = canonicalID
					if parentID, okParent := pickOpts.Metadata[cliproxyexecutor.ParentSessionIDMetadataKey]; okParent && parentID != canonicalID {
						meta[cliproxyexecutor.ParentSessionIDMetadataKey] = parentID
					} else {
						delete(meta, cliproxyexecutor.ParentSessionIDMetadataKey)
					}
					execOpts.Metadata = meta
				}
			}
			payload := execOpts.OriginalRequest
			if len(payload) == 0 {
				payload = execReq.Payload
			}
			execOpts.Metadata = ensureCanonicalSessionMetadata(execOpts.Metadata, execOpts.Headers, payload)
			var errIntercept error
			execReq, execOpts, errIntercept = applyRequestAfterAuthInterceptor(execCtx, executor, provider, execReq, execOpts, requestedModelAliasFromOptions(execOpts, routeModel))
			if errIntercept != nil {
				return cliproxyexecutor.Response{}, errIntercept
			}
			execReq = attachResolvedExecutionModelInfo(routing, execReq, auth, routeModel, upstreamModel, restoreExecutionModel)
			execCtx = syncMetadataSessionToContext(execCtx, execOpts.Metadata)
			executionSeq := m.nextExecutionSeq()
			startExec := time.Now()
			resp, errExec := executor.CountTokens(execCtx, auth, execReq, execOpts)
			errExec = markUpstreamExecutionAttemptFromContext(execCtx, errExec)
			durationExec := time.Since(startExec)
			if errExec != nil {
				if hasUpstreamExecutionAttempt(errExec) {
					upstreamErr = errExec
				}
				if errCtx := execCtx.Err(); errCtx != nil {
					return cliproxyexecutor.Response{}, errCtx
				}
				refreshCtx := newUpstreamAttemptContext(execCtx)
				if refreshed, okRefresh := m.tryRefreshAfterUnauthorized(refreshCtx, auth, errExec, didRefreshOnUnauthorized, execOpts.Headers); okRefresh {
					auth = refreshed
					didRefreshOnUnauthorized = true
					execCtx = newUpstreamAttemptContext(execCtx)
					execCtx = syncMetadataSessionToContext(execCtx, execOpts.Metadata)
					executionSeq = m.nextExecutionSeq()
					startRetry := time.Now()
					resp, errExec = executor.CountTokens(execCtx, auth, execReq, execOpts)
					errExec = markUpstreamExecutionAttemptFromContext(execCtx, errExec)
					durationRetry := time.Since(startRetry)
					if errExec != nil {
						if hasUpstreamExecutionAttempt(errExec) {
							upstreamErr = errExec
						}
						warnLogUpstreamFailure(execCtx, entry, provider, upstreamModel, auth, durationRetry, errExec)
						if errCtx := execCtx.Err(); errCtx != nil {
							return cliproxyexecutor.Response{}, errCtx
						}
					}
				} else {
					warnLogUpstreamFailure(execCtx, entry, provider, upstreamModel, auth, durationExec, errExec)
				}
			}
			if errCancel := claudeOAuthRequestCancellation(execCtx, auth, errExec); errCancel != nil {
				return cliproxyexecutor.Response{}, errCancel
			}
			result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: errExec == nil, Options: execOpts, SkipQuotaObservation: true, AccessToken: authAccessTokenForRequest(execCtx, auth, execOpts.Headers), CredentialVersion: auth.CredentialVersion, RegistrationEpoch: auth.RegistrationEpoch, ExecutionSeq: executionSeq}
			if errExec != nil {
				result.Error = resultErrorFromError(errExec)
				if ra := retryAfterFromError(errExec); ra != nil {
					result.RetryAfter = ra
				}
				action, okAction := matchRequestScopedErrorAction(auth, errExec, m.runtimeConfigSnapshot())
				applyRequestScopedActionToResult(action, okAction, &result)
				// Some Anthropic-compatible upstreams and plugin executors do not
				// implement count_tokens and return a generic endpoint 404 or an
				// explicit 501 / unsupported-capability error. Record the failure
				// for hooks and metrics without suspending a model that remains
				// usable through the messages endpoint.
				if isCountTokensEndpointNotFoundError(errExec, execReq.Model) && (result.Error == nil || result.Error.Code != ErrorCodeForceCooldown) {
					m.recordAvailabilityNeutralResult(execCtx, result)
				} else {
					if isCredentialScopedError(errExec) {
						result.CredentialScope = true
					}
					m.MarkResult(execCtx, result)
					errExec = annotateTrustedRetryAfterByHint(errExec)
					syncPreferredUpstreamError(&upstreamErr, errExec)
				}
				if okAction {
					if isRequestScopedStop(action, okAction) {
						return cliproxyexecutor.Response{}, wrapRequestStopError(errExec)
					}
					authErr = errExec
					if result.CredentialScope {
						break
					}
					continue
				}
				if isRequestInvalidError(errExec) {
					return cliproxyexecutor.Response{}, errExec
				}
				authErr = errExec
				if result.CredentialScope {
					break
				}
				continue
			}
			m.MarkResult(execCtx, result)
			attemptAliasResult := resolveAttemptAliasResult(routing, auth, routeModel, upstreamModel, aliasResult)
			rewriteForceMappedResponse(&resp, attemptAliasResult)
			return resp, nil
		}
		if authErr != nil {
			action, okAction := matchRequestScopedErrorAction(auth, authErr, m.runtimeConfigSnapshot())
			if okAction {
				if isRequestScopedStop(action, okAction) {
					return cliproxyexecutor.Response{}, wrapRequestStopError(authErr)
				}
				lastErr = authErr
				continue
			}
			if isRequestInvalidError(authErr) {
				return cliproxyexecutor.Response{}, authErr
			}
			lastErr = authErr
			continue
		}
	}
}

func (m *Manager) executeStreamMixedOnce(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, maxRetryCredentials int, retryRound int, defaultRequestRetry int) (*cliproxyexecutor.StreamResult, error) {
	if len(providers) == 0 {
		return nil, &Error{Code: "provider_not_found", Message: "no provider supplied"}
	}
	routeModel := authSelectionModelFromOptions(opts, req.Model)
	executionModel, restoreExecutionModel := executionModelForAuthSelection(opts, req.Model)
	opts = ensureRequestedModelMetadata(opts, routeModel)
	tried := make(map[string]struct{})

	for authID := range m.requestRetryRoundExclusions(retryRound, defaultRequestRetry) {
		tried[authID] = struct{}{}
	}
	attempted := make(map[string]struct{})
	var lastErr error
	var upstreamErr error
	for {
		if maxRetryCredentials > 0 && len(attempted) >= maxRetryCredentials {
			if lastErr != nil {
				preferredErr := preferredExecutionAttemptError(lastErr, upstreamErr)
				return nil, preferredErr
			}
			return nil, &Error{Code: "auth_not_found", Message: "no auth available"}
		}
		opts.Metadata = cloneRequestMetadata(opts.Metadata)
		pickOpts := opts

		auth, executor, provider, errPick := m.pickNextMixed(ctx, providers, routeModel, pickOpts, tried)
		if errPick != nil {
			preferredErr := preferredExecutionAttemptError(lastErr, upstreamErr)
			if shouldReturnLastErrorOnPickFailure(lastErr, errPick) {
				return nil, preferredErr
			}
			return nil, errPick
		}
		if auth == nil || executor == nil {
			return nil, &Error{Code: "executor_not_found", Message: "executor not registered"}
		}

		entry := logEntryWithRequestID(ctx)
		debugLogAuthSelection(entry, auth, provider, routeModel)
		publishSelectedAuthMetadata(opts.Metadata, auth)

		tried[auth.ID] = struct{}{}
		execCtx := ctx
		if rt := m.roundTripperFor(auth); rt != nil {
			execCtx = context.WithValue(execCtx, roundTripperContextKey{}, rt)
			execCtx = context.WithValue(execCtx, "cliproxy.roundtripper", rt)
		}
		// Enrich before auth preparation so prepare-stage usage records observe the client request.
		execCtx = contextWithRequestedModelAlias(execCtx, pickOpts, routeModel)
		execCtx = coreusage.WithSelectionID(execCtx, selectionIDFromOptions(pickOpts))
		execCtx = newUpstreamAttemptContext(execCtx)
		models, pooled, aliasResult, routing := m.executionModelCandidatesWithAlias(auth, routeModel)
		models = m.filterExecutionModelsForRequest(execCtx, auth, routeModel, models, pooled, pickOpts.Headers)
		if len(models) == 0 {
			continue
		}
		attempted[auth.ID] = struct{}{}
		auth, errPrepare := m.prepareRequestAuth(execCtx, executor, auth)
		if errPrepare != nil {

			if errCancel := claudeOAuthRequestCancellation(execCtx, auth, errPrepare); errCancel != nil {
				return nil, errCancel
			}
			stateModel := m.selectionModelKeyForAuth(auth, routeModel)
			if stateModel == "" {
				stateModel = canonicalModelKey(routeModel)
			}
			result := Result{AuthID: auth.ID, Provider: provider, Model: stateModel, RouteModel: routeModel, Success: false, Error: resultErrorFromError(errPrepare), Options: pickOpts, AccessToken: authAccessTokenForRequest(execCtx, auth, pickOpts.Headers), CredentialVersion: auth.CredentialVersion, RegistrationEpoch: auth.RegistrationEpoch}
			m.MarkResult(execCtx, result)
			lastErr = errPrepare
			continue
		}
		execReq := sanitizeDownstreamWebsocketFallbackRequest(execCtx, auth, req)
		streamExecutionModel := ""
		if restoreExecutionModel {
			streamExecutionModel = executionModel
		}
		execOpts := opts
		payload := execOpts.OriginalRequest
		if len(payload) == 0 {
			payload = execReq.Payload
		}
		execOpts.Metadata = ensureCanonicalSessionMetadata(execOpts.Metadata, execOpts.Headers, payload)
		execCtx = syncMetadataSessionToContext(execCtx, execOpts.Metadata)
		streamResult, errStream := m.executeStreamWithModelPool(execCtx, executor, auth, provider, execReq, execOpts, routeModel, streamExecutionModel, models, pooled, aliasResult, routing, true, false)
		if errStream != nil {
			if hasUpstreamExecutionAttempt(errStream) {
				upstreamErr = errStream
			}
			action, okAction := matchRequestScopedErrorAction(auth, errStream, m.runtimeConfigSnapshot())
			if isRequestScopedStop(action, okAction) || (!okAction && (isRequestInvalidError(errStream) || cliproxyexecutor.IsUpstreamWebsocketTurnError(errStream))) {
				if isRequestScopedStop(action, okAction) {
					return nil, wrapRequestStopError(errStream)
				}
				return nil, errStream
			}
			if errCtx := execCtx.Err(); errCtx != nil && ctx != nil && ctx.Err() != nil {
				return nil, errCtx
			}
			if okAction {
				lastErr = errStream
				continue
			}
			lastErr = errStream
			continue
		}
		return streamResult, nil
	}
}

func withAttemptedAuthTracker(opts cliproxyexecutor.Options, attempted map[string]struct{}) cliproxyexecutor.Options {
	if attempted == nil {
		return opts
	}
	meta := cloneRequestMetadata(opts.Metadata)
	prevCallback, _ := meta[cliproxyexecutor.SelectedAuthCallbackMetadataKey].(func(string))
	meta[cliproxyexecutor.SelectedAuthCallbackMetadataKey] = func(authID string) {
		if strings.TrimSpace(authID) != "" {
			attempted[authID] = struct{}{}
		}
		if prevCallback != nil {
			prevCallback(authID)
		}
	}
	opts.Metadata = meta
	return opts
}

func cloneRequestMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return make(map[string]any, 4)
	}
	dst := make(map[string]any, len(src)+4)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func ensureRequestedModelMetadata(opts cliproxyexecutor.Options, requestedModel string) cliproxyexecutor.Options {
	opts.Metadata = cloneRequestMetadata(opts.Metadata)
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return opts
	}
	if hasRequestedModelMetadata(opts.Metadata) {
		return opts
	}
	opts.Metadata[cliproxyexecutor.RequestedModelMetadataKey] = requestedModel
	return opts
}

func authSelectionModelFromOptions(opts cliproxyexecutor.Options, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if len(opts.Metadata) == 0 {
		return fallback
	}
	raw, ok := opts.Metadata[cliproxyexecutor.AuthSelectionModelMetadataKey]
	if !ok || raw == nil {
		return fallback
	}
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	case []byte:
		if strings.TrimSpace(string(value)) != "" {
			return strings.TrimSpace(string(value))
		}
	}
	return fallback
}

func executionModelForAuthSelection(opts cliproxyexecutor.Options, model string) (string, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}
	selectionModel := authSelectionModelFromOptions(opts, model)
	if selectionModel == model {
		return "", false
	}
	return model, true
}

func hasRequestedModelMetadata(meta map[string]any) bool {
	if len(meta) == 0 {
		return false
	}
	raw, ok := meta[cliproxyexecutor.RequestedModelMetadataKey]
	if !ok || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []byte:
		return strings.TrimSpace(string(v)) != ""
	default:
		return false
	}
}

type requestAuthPrepareLock struct {
	mu sync.Mutex
}

func (m *Manager) prepareRequestAuth(ctx context.Context, executor ProviderExecutor, auth *Auth) (*Auth, error) {
	if m == nil || executor == nil || auth == nil {
		return auth, nil
	}
	preparer, ok := executor.(RequestAuthPreparer)
	if !ok {
		return auth, nil
	}

	return m.PrepareRequestAuth(ctx, preparer, auth)
}

// PrepareRequestAuth prepares a registered credential using the same serialization
// and lifecycle checks as normal request execution. Management tools use this path too.
func (m *Manager) PrepareRequestAuth(ctx context.Context, preparer RequestAuthPreparer, auth *Auth) (*Auth, error) {
	if m == nil || preparer == nil || auth == nil || !preparer.ShouldPrepareRequestAuth(auth) {
		return auth, nil
	}

	id := strings.TrimSpace(auth.ID)
	if id == "" {
		return preparer.PrepareRequestAuth(ctx, auth.Clone())
	}

	isMeta := authProviderIs(auth, "meta")
	var prepareMu *sync.Mutex
	if isMeta {
		// Meta also mints on 401 recovery. Serialize both paths per credential.
		lockValue, _ := m.refreshLocks.LoadOrStore(id, &authRefreshLock{})
		prepareMu = &lockValue.(*authRefreshLock).mu
	} else {
		lockValue, _ := m.requestPrepareLocks.LoadOrStore(id, &requestAuthPrepareLock{})
		prepareMu = &lockValue.(*requestAuthPrepareLock).mu
	}
	prepareMu.Lock()
	defer prepareMu.Unlock()

	target := auth.Clone()
	m.mu.RLock()
	current := m.auths[id]
	if current != nil {
		target = current.Clone()
	}
	m.mu.RUnlock()
	if current == nil && isMeta {
		return nil, fmt.Errorf("prepare meta auth: credential no longer registered")
	}

	if !preparer.ShouldPrepareRequestAuth(target) {
		return target, nil
	}

	base := target.Clone()
	updated, errPrepare := preparer.PrepareRequestAuth(ctx, base.Clone())
	if errPrepare != nil {
		return auth, errPrepare
	}
	if updated == nil {
		return target, nil
	}

	saved, errUpdate := m.UpdatePreparedAuth(ctx, base, updated)
	if errUpdate != nil {
		return nil, errUpdate
	}
	if saved != nil {
		return saved, nil
	}
	if isMeta {
		return nil, fmt.Errorf("prepare meta auth: credential removed during mint")
	}
	return target, nil
}

func contextWithRequestedModelAlias(ctx context.Context, opts cliproxyexecutor.Options, fallback string) context.Context {
	alias := requestedModelAliasFromOptions(opts, fallback)
	ctx = coreusage.WithRequestedModelAlias(ctx, alias)
	effort := reasoningEffortFromOptions(opts)
	if effort != "" {
		ctx = coreusage.WithReasoningEffort(ctx, effort)
	}
	serviceTier := serviceTierFromOptions(opts)
	if serviceTier != "" {
		ctx = coreusage.WithServiceTier(ctx, serviceTier)
	}
	if generate, ok := generateFromOptions(opts); ok {
		ctx = coreusage.WithGenerate(ctx, generate)
	}
	ctx = coreusage.WithStream(ctx, opts.Stream)
	if selectionID := selectionIDFromOptions(opts); selectionID != "" {
		ctx = coreusage.WithSelectionID(ctx, selectionID)
	} else if _, exists := opts.Metadata[cliproxyexecutor.SelectionIDMetadataKey]; exists {
		ctx = coreusage.WithSelectionID(ctx, "")
	}
	return ctx
}

func selectionIDFromOptions(opts cliproxyexecutor.Options) string {
	return stringMetadataValue(opts.Metadata, cliproxyexecutor.SelectionIDMetadataKey)
}

func requestedModelAliasFromOptions(opts cliproxyexecutor.Options, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if len(opts.Metadata) == 0 {
		return fallback
	}
	raw, ok := opts.Metadata[cliproxyexecutor.RequestedModelMetadataKey]
	if !ok || raw == nil {
		return fallback
	}
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return fallback
		}
		return strings.TrimSpace(value)
	case []byte:
		if len(value) == 0 {
			return fallback
		}
		return strings.TrimSpace(string(value))
	default:
		return fallback
	}
}

func reasoningEffortFromOptions(opts cliproxyexecutor.Options) string {
	if len(opts.Metadata) == 0 {
		return ""
	}
	raw, ok := opts.Metadata[cliproxyexecutor.ReasoningEffortMetadataKey]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

func serviceTierFromOptions(opts cliproxyexecutor.Options) string {
	return stringMetadataValue(opts.Metadata, cliproxyexecutor.ServiceTierMetadataKey)
}

func generateFromOptions(opts cliproxyexecutor.Options) (bool, bool) {
	if len(opts.Metadata) == 0 {
		return false, false
	}
	raw, ok := opts.Metadata[cliproxyexecutor.GenerateMetadataKey]
	if !ok || raw == nil {
		return false, false
	}
	switch value := raw.(type) {
	case bool:
		return value, true
	default:
		return false, false
	}
}

func stringMetadataValue(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

func pinnedAuthIDFromMetadata(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	raw, ok := meta[cliproxyexecutor.PinnedAuthMetadataKey]
	if !ok || raw == nil {
		return ""
	}
	switch val := raw.(type) {
	case string:
		return strings.TrimSpace(val)
	case []byte:
		return strings.TrimSpace(string(val))
	default:
		return ""
	}
}

func disallowFreeAuthFromMetadata(meta map[string]any) bool {
	if len(meta) == 0 {
		return false
	}
	raw, ok := meta[cliproxyexecutor.DisallowFreeAuthMetadataKey]
	if !ok || raw == nil {
		return false
	}
	switch val := raw.(type) {
	case bool:
		return val
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(val))
		return err == nil && parsed
	case []byte:
		parsed, err := strconv.ParseBool(strings.TrimSpace(string(val)))
		return err == nil && parsed
	default:
		return false
	}
}

func isFreeCodexAuth(auth *Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	if !authProviderIs(auth, "codex") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(auth.Attributes["plan_type"]), "free")
}

func publishSelectedAuthMetadata(meta map[string]any, auth *Auth) {
	if len(meta) == 0 || auth == nil {
		return
	}
	if authID := strings.TrimSpace(auth.ID); authID != "" {
		meta[cliproxyexecutor.SelectedAuthMetadataKey] = authID
		if callback, ok := meta[cliproxyexecutor.SelectedAuthCallbackMetadataKey].(func(string)); ok && callback != nil {
			callback(authID)
		}
	}
	if authIndex := strings.TrimSpace(auth.EnsureIndex()); authIndex != "" {
		meta[cliproxyexecutor.SelectedAuthIndexMetadataKey] = authIndex
		if callback, ok := meta[cliproxyexecutor.SelectedAuthIndexCallbackMetadataKey].(func(string)); ok && callback != nil {
			callback(authIndex)
		}
	}
}

func (m *Manager) executorFor(provider string) ProviderExecutor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	exec, _ := m.executorLocked(provider)
	return exec
}

// roundTripperContextKey is an unexported context key type to avoid collisions.
type roundTripperContextKey struct{}

// roundTripperFor retrieves an HTTP RoundTripper for the given auth if a provider is registered.
func (m *Manager) roundTripperFor(auth *Auth) http.RoundTripper {
	m.mu.RLock()
	p := m.rtProvider
	m.mu.RUnlock()
	if p == nil || auth == nil {
		return nil
	}
	return p.RoundTripperFor(auth)
}

// RoundTripperProvider defines a minimal provider of per-auth HTTP transports.
type RoundTripperProvider interface {
	RoundTripperFor(auth *Auth) http.RoundTripper
}

// RequestPreparer is an optional interface that provider executors can implement
// to mutate outbound HTTP requests with provider credentials.
type RequestPreparer interface {
	PrepareRequest(req *http.Request, auth *Auth) error
}

func executorKeyFromAuth(auth *Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		providerKey := strings.TrimSpace(auth.Attributes["provider_key"])
		compatName := strings.TrimSpace(auth.Attributes["compat_name"])
		if compatName != "" {
			if providerKey == "" {
				providerKey = compatName
			}
			return util.OpenAICompatibleProviderKey(providerKey)
		}
	}
	if authProviderIs(auth, "openai-compatibility") {
		providerKey := strings.TrimSpace(auth.Label)
		if providerKey == "" {
			providerKey = "openai-compatibility"
		}
		return util.OpenAICompatibleProviderKey(providerKey)
	}
	return strings.ToLower(strings.TrimSpace(auth.Provider))
}

// logEntryWithRequestID returns a logrus entry with request_id field if available in context.
func logEntryWithRequestID(ctx context.Context) *log.Entry {
	if ctx == nil {
		return log.NewEntry(log.StandardLogger())
	}
	if reqID := logging.GetRequestID(ctx); reqID != "" {
		return log.WithField("request_id", reqID)
	}
	return log.NewEntry(log.StandardLogger())
}

func debugLogAuthSelection(entry *log.Entry, auth *Auth, provider string, model string) {
	if !log.IsLevelEnabled(log.DebugLevel) {
		return
	}
	if entry == nil || auth == nil {
		return
	}
	accountType, accountInfo := auth.AccountInfo()
	proxyInfo := auth.ProxyInfo()
	suffix := ""
	if proxyInfo != "" {
		suffix = " " + proxyInfo
	}
	switch accountType {
	case "api_key":
		entry.Debugf("Use API key %s for model %s%s", util.HideAPIKey(accountInfo), model, suffix)
	case "oauth":
		ident := formatOauthIdentity(auth, provider, accountInfo)
		entry.Debugf("Use OAuth %s for model %s%s", ident, model, suffix)
	}
}

func formatOauthIdentity(auth *Auth, provider string, accountInfo string) string {
	if auth == nil {
		return ""
	}
	// Prefer the auth's provider when available.
	providerName := strings.TrimSpace(auth.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(provider)
	}
	// Only log the basename to avoid leaking host paths.
	// FileName may be unset for some auth backends; fall back to ID.
	authFile := strings.TrimSpace(auth.FileName)
	if authFile == "" {
		authFile = strings.TrimSpace(auth.ID)
	}
	if authFile != "" {
		authFile = filepath.Base(authFile)
	}
	parts := make([]string, 0, 3)
	if providerName != "" {
		parts = append(parts, "provider="+providerName)
	}
	if authFile != "" {
		parts = append(parts, "auth_file="+authFile)
	}
	if len(parts) == 0 {
		return accountInfo
	}
	return strings.Join(parts, " ")
}

func formatAuthIdentity(auth *Auth, provider string) string {
	if auth == nil {
		return "auth=nil"
	}
	accountType, accountInfo := auth.AccountInfo()
	switch accountType {
	case "api_key":
		return fmt.Sprintf("api_key=%s", util.HideAPIKey(accountInfo))
	case "oauth":
		return formatOauthIdentity(auth, provider, accountInfo)
	default:
		if auth.FileName != "" {
			return fmt.Sprintf("auth_file=%s", filepath.Base(auth.FileName))
		}
		if auth.ID != "" {
			return fmt.Sprintf("auth_id=%s", auth.ID)
		}
		if accountInfo != "" {
			return accountInfo
		}
		return "unknown"
	}
}

func safeErrorDiagnosticForLog(err error) string {
	if err == nil {
		return ""
	}
	diagnostic := err.Error()
	type logDiagnosticError interface {
		LogDiagnostic() string
	}
	var diagnosticErr logDiagnosticError
	if errors.As(err, &diagnosticErr) && diagnosticErr != nil {
		if markedDiagnostic := strings.TrimSpace(diagnosticErr.LogDiagnostic()); markedDiagnostic != "" {
			diagnostic = markedDiagnostic
		}
	}
	return logging.SafeDiagnosticForLog(diagnostic)
}

func warnLogUpstreamFailure(ctx context.Context, entry *log.Entry, provider, model string, auth *Auth, duration time.Duration, err error) {
	if err == nil {
		return
	}
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if isRequestInvalidError(err) {
		return
	}
	if entry == nil {
		if ctx != nil {
			entry = logEntryWithRequestID(ctx)
		} else {
			entry = log.NewEntry(log.StandardLogger())
		}
	}
	authIdent := formatAuthIdentity(auth, provider)
	errSummary := safeErrorDiagnosticForLog(err)
	duration = duration.Round(time.Millisecond)
	if statusCode := statusCodeFromError(err); statusCode != 0 {
		entry.Warnf("%3d | %13v | upstream execution failed: provider=%s model=%s auth=%s err=%s", statusCode, duration, provider, model, authIdent, errSummary)
		return
	}
	entry.Warnf("upstream execution failed: provider=%s model=%s auth=%s duration=%s err=%s", provider, model, authIdent, duration, errSummary)
}

// InjectCredentials delegates per-provider HTTP request preparation when supported.
// If the registered executor for the auth provider implements RequestPreparer,
// it will be invoked to modify the request (e.g., add headers).
func (m *Manager) InjectCredentials(req *http.Request, authID string) error {
	if req == nil || authID == "" {
		return nil
	}
	m.mu.RLock()
	a := m.auths[authID]
	var exec ProviderExecutor
	if a != nil {
		exec, _ = m.executorLocked(executorKeyFromAuth(a))
	}
	m.mu.RUnlock()
	if a == nil || exec == nil {
		return nil
	}
	if p, ok := exec.(RequestPreparer); ok && p != nil {
		return p.PrepareRequest(req, a)
	}
	return nil
}

// PrepareHttpRequest injects provider credentials into the supplied HTTP request.
func (m *Manager) PrepareHttpRequest(ctx context.Context, auth *Auth, req *http.Request) error {
	if m == nil {
		return &Error{Code: "provider_not_found", Message: "manager is nil"}
	}
	if auth == nil {
		return &Error{Code: "auth_not_found", Message: "auth is nil"}
	}
	if req == nil {
		return &Error{Code: "invalid_request", Message: "http request is nil"}
	}
	if ctx != nil {
		*req = *req.WithContext(ctx)
	}
	providerKey := executorKeyFromAuth(auth)
	if providerKey == "" {
		return &Error{Code: "provider_not_found", Message: "auth provider is empty"}
	}
	exec := m.executorFor(providerKey)
	if exec == nil {
		return &Error{Code: "provider_not_found", Message: "executor not registered for provider: " + providerKey}
	}
	preparer, ok := exec.(RequestPreparer)
	if !ok || preparer == nil {
		return &Error{Code: "not_supported", Message: "executor does not support http request preparation"}
	}
	return preparer.PrepareRequest(req, auth)
}

// NewHttpRequest constructs a new HTTP request and injects provider credentials into it.
func (m *Manager) NewHttpRequest(ctx context.Context, auth *Auth, method, targetURL string, body []byte, headers http.Header) (*http.Request, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	method = strings.TrimSpace(method)
	if method == "" {
		method = http.MethodGet
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, targetURL, reader)
	if err != nil {
		return nil, err
	}
	if headers != nil {
		httpReq.Header = headers.Clone()
	}
	if errPrepare := m.PrepareHttpRequest(ctx, auth, httpReq); errPrepare != nil {
		return nil, errPrepare
	}
	return httpReq, nil
}

// HttpRequest injects provider credentials into the supplied HTTP request and executes it.
func (m *Manager) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	if m == nil {
		return nil, &Error{Code: "provider_not_found", Message: "manager is nil"}
	}
	if auth == nil {
		return nil, &Error{Code: "auth_not_found", Message: "auth is nil"}
	}
	if req == nil {
		return nil, &Error{Code: "invalid_request", Message: "http request is nil"}
	}
	providerKey := executorKeyFromAuth(auth)
	if providerKey == "" {
		return nil, &Error{Code: "provider_not_found", Message: "auth provider is empty"}
	}
	exec := m.executorFor(providerKey)
	if exec == nil {
		return nil, &Error{Code: "provider_not_found", Message: "executor not registered for provider: " + providerKey}
	}
	return exec.HttpRequest(ctx, auth, req)
}

func ensureCanonicalSessionMetadata(metadata map[string]any, headers http.Header, payload []byte) map[string]any {
	if metadata != nil {
		if canonicalID, ok := metadata[cliproxyexecutor.CanonicalSessionIDMetadataKey].(string); ok && strings.TrimSpace(canonicalID) != "" {
			return metadata
		}
	}
	canonicalID := CanonicalSessionID(headers, payload, metadata)
	if canonicalID == "" {
		return metadata
	}
	out := make(map[string]any, len(metadata)+1)
	for k, v := range metadata {
		out[k] = v
	}
	out[cliproxyexecutor.CanonicalSessionIDMetadataKey] = canonicalID
	return out
}

func syncMetadataSessionToContext(ctx context.Context, metadata map[string]any) context.Context {
	if ctx == nil {
		return nil
	}
	canonicalID := ""
	if len(metadata) > 0 {
		canonicalID, _ = metadata[cliproxyexecutor.CanonicalSessionIDMetadataKey].(string)
		if canonicalID == "" {
			canonicalID, _ = metadata[cliproxyexecutor.LCPAffinitySessionIDMetadataKey].(string)
		}
		if canonicalID == "" {
			if execID, _ := metadata[cliproxyexecutor.ExecutionSessionMetadataKey].(string); execID != "" {
				execID = strings.TrimSpace(execID)
				if !strings.HasPrefix(execID, "execution:") {
					canonicalID = "execution:" + execID
				} else {
					canonicalID = execID
				}
			}
		}
		if canonicalID == "" {
			if derivedID, _ := metadata[cliproxyexecutor.DerivedSessionIDMetadataKey].(string); derivedID != "" {
				derivedID = strings.TrimSpace(derivedID)
				if !strings.HasPrefix(derivedID, "derived:") {
					canonicalID = "derived:" + derivedID
				} else {
					canonicalID = derivedID
				}
			}
		}
	}
	canonicalID = strings.TrimSpace(canonicalID)
	if canonicalID == "" {
		clientMeta := logging.GetClientRequestMetadata(ctx)
		if clientMeta.SessionID != "" || clientMeta.ParentSessionID != "" || clientMeta.NodeKind != "" || clientMeta.IsFork || clientMeta.IsCompaction {
			clientMeta.SessionID = ""
			clientMeta.ParentSessionID = ""
			clientMeta.NodeKind = ""
			clientMeta.IsFork = false
			clientMeta.IsCompaction = false
			ctx = logging.WithClientRequestMetadata(ctx, clientMeta)
		}
		return util.WithSessionID(ctx, "")
	}
	clientMeta := logging.GetClientRequestMetadata(ctx)
	clientMeta.SessionID = cliproxysession.BoundSessionIdentity(canonicalID)
	if parentID, ok := metadata[cliproxyexecutor.ParentSessionIDMetadataKey].(string); ok && strings.TrimSpace(parentID) != "" {
		clientMeta.ParentSessionID = cliproxysession.BoundSessionIdentity(strings.TrimSpace(parentID))
	} else {
		clientMeta.ParentSessionID = ""
	}
	if clientMeta.SessionID == clientMeta.ParentSessionID {
		clientMeta.ParentSessionID = ""
	}
	if nodeKind, ok := metadata[cliproxyexecutor.NodeKindMetadataKey].(string); ok && strings.TrimSpace(nodeKind) != "" {
		clientMeta.NodeKind = strings.TrimSpace(nodeKind)
	} else {
		clientMeta.NodeKind = ""
	}
	if isFork, ok := metadata[cliproxyexecutor.IsForkMetadataKey].(bool); ok {
		clientMeta.IsFork = isFork
	} else {
		clientMeta.IsFork = false
	}
	if isCompaction, ok := metadata[cliproxyexecutor.IsCompactionMetadataKey].(bool); ok {
		clientMeta.IsCompaction = isCompaction
	} else {
		clientMeta.IsCompaction = false
	}
	ctx = logging.WithClientRequestMetadata(ctx, clientMeta)
	return util.WithSessionID(ctx, clientMeta.SessionID)
}

// VerifyInference performs an operator-triggered, account-pinned inference verification
// using an isolated session and minimal non-user prompt/token budget. It never falls back
// to another credential and requires a completed response before marking InferenceVerified.
func (m *Manager) VerifyInference(ctx context.Context, id, model string) (*Auth, error) {
	if m == nil {
		return nil, &Error{Code: "manager_unavailable", Message: "auth manager is not initialized", HTTPStatus: http.StatusServiceUnavailable}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, &Error{Code: "invalid_request", Message: "auth id is required", HTTPStatus: http.StatusBadRequest}
	}
	auth, ok := m.GetByID(id)
	if !ok || auth == nil {
		return nil, &Error{Code: "auth_not_found", Message: "credential not found", HTTPStatus: http.StatusNotFound}
	}
	if auth.Disabled || auth.Status == StatusDisabled {
		return auth, &Error{Code: "auth_disabled", Message: "credential is disabled", HTTPStatus: http.StatusConflict}
	}
	if auth.RequiresSignIn {
		return auth, &Error{Code: "sign_in_required", Message: "credential requires sign-in before inference verification", HTTPStatus: http.StatusConflict}
	}
	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	m.mu.RLock()
	executor := m.executors[provider]
	m.mu.RUnlock()
	if executor == nil {
		return auth, &Error{Code: "executor_not_found", Message: "executor not registered for provider", HTTPStatus: http.StatusBadRequest}
	}
	executor = executorForAuth(executor, auth)

	routeModel := strings.TrimSpace(model)
	if routeModel == "" {
		if models := registry.GetGlobalRegistry().GetModelsForClient(auth.ID); len(models) > 0 && models[0] != nil && strings.TrimSpace(models[0].ID) != "" {
			routeModel = strings.TrimSpace(models[0].ID)
		} else {
			routeModel = "gpt-5"
		}
	}
	verifyModel := routeModel
	if candidates, _, _, _ := m.executionModelCandidatesWithAlias(auth, routeModel); len(candidates) > 0 && strings.TrimSpace(candidates[0]) != "" {
		verifyModel = strings.TrimSpace(candidates[0])
	}
	stateModel := m.selectionModelKeyForAuth(auth, routeModel)
	if stateModel == "" {
		stateModel = canonicalModelKey(routeModel)
	}

	sessionID := fmt.Sprintf("verify-%s-%d", auth.ID, time.Now().UTC().UnixNano())
	// Codex upstreams reject integer max_output_tokens below 16, so the minimal
	// verification budget must sit at that floor.
	payload := []byte(fmt.Sprintf(`{"model":%q,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ping"}]}],"max_output_tokens":16,"store":false}`, verifyModel))
	req := cliproxyexecutor.Request{
		Model:   verifyModel,
		Payload: payload,
	}
	opts := cliproxyexecutor.Options{
		Stream:          true,
		SourceFormat:    sdktranslator.FormatOpenAIResponse,
		ResponseFormat:  sdktranslator.FormatOpenAIResponse,
		OriginalRequest: payload,
		Headers:         http.Header{"X-Session-Id": {sessionID}},
		Metadata:        map[string]any{cliproxyexecutor.CanonicalSessionIDMetadataKey: sessionID},
	}

	execCtx := newUpstreamAttemptContext(ctx)
	execCtx = syncMetadataSessionToContext(execCtx, opts.Metadata)
	executionSeq := m.nextExecutionSeq()
	accessToken := authAccessTokenForRequest(execCtx, auth, opts.Headers)
	credVersion := auth.CredentialVersion
	regEpoch := auth.RegistrationEpoch

	streamRes, errStream := executor.ExecuteStream(execCtx, auth, req, opts)
	if errStream != nil {
		rerr := resultErrorFromError(errStream)
		if rerr != nil && rerr.HTTPStatus == http.StatusNotImplemented {
			opts.Stream = false
			resp, errExec := executor.Execute(execCtx, auth, req, opts)
			rerrExec := resultErrorFromError(errExec)
			if rerrExec != nil {
				rerrExec.Message = sanitizeRenewalMessage(rerrExec.Message)
			}
			if errExec != nil {
				m.MarkResult(execCtx, Result{
					AuthID:            auth.ID,
					Provider:          provider,
					Model:             stateModel,
					RouteModel:        routeModel,
					Success:           false,
					Error:             rerrExec,
					RetryAfter:        retryAfterFromError(errExec),
					CredentialScope:   isCredentialScopedError(errExec),
					Options:           opts,
					AccessToken:       accessToken,
					CredentialVersion: credVersion,
					RegistrationEpoch: regEpoch,
					ExecutionSeq:      executionSeq,
				})
				updated, _ := m.GetByID(auth.ID)
				return updated, errExec
			}
			if resp.Incomplete || !isCompletedExecutionPayload(resp.Payload) {
				incompleteErr := &Error{Code: "incomplete_response", Message: "verification response did not complete", HTTPStatus: http.StatusBadGateway}
				updated, _ := m.GetByID(auth.ID)
				return updated, incompleteErr
			}
			m.MarkResult(execCtx, Result{
				AuthID:            auth.ID,
				Provider:          provider,
				Model:             stateModel,
				RouteModel:        routeModel,
				Success:           true,
				CompletedResponse: true,
				Options:           opts,
				AccessToken:       accessToken,
				CredentialVersion: credVersion,
				RegistrationEpoch: regEpoch,
				ExecutionSeq:      executionSeq,
			})
			updated, okUpdated := m.GetByID(auth.ID)
			if !okUpdated || updated == nil {
				return nil, &Error{Code: "auth_not_found", Message: "credential was removed during verification", HTTPStatus: http.StatusConflict}
			}
			if updated.CredentialVersion != credVersion || updated.RegistrationEpoch != regEpoch || !updated.InferenceVerified {
				return updated, &Error{Code: "credential_superseded", Message: "credential was replaced during verification; re-run verification for the new credential", HTTPStatus: http.StatusConflict}
			}
			return updated, nil
		}
		if rerr != nil {
			rerr.Message = sanitizeRenewalMessage(rerr.Message)
		}
		m.MarkResult(execCtx, Result{
			AuthID:            auth.ID,
			Provider:          provider,
			Model:             stateModel,
			RouteModel:        routeModel,
			Success:           false,
			Error:             rerr,
			RetryAfter:        retryAfterFromError(errStream),
			CredentialScope:   isCredentialScopedError(errStream),
			Options:           opts,
			AccessToken:       accessToken,
			CredentialVersion: credVersion,
			RegistrationEpoch: regEpoch,
			ExecutionSeq:      executionSeq,
		})
		updated, _ := m.GetByID(auth.ID)
		return updated, errStream
	}

	var sawCompleted bool
	var sawIncomplete bool
	var chunkErr error
	if streamRes != nil && streamRes.Chunks != nil {
		for chunk := range streamRes.Chunks {
			if chunk.Err != nil && chunkErr == nil {
				chunkErr = chunk.Err
			}
			if chunk.Incomplete {
				sawIncomplete = true
			}
			if len(chunk.Payload) > 0 {
				comp, incomp := streamPayloadCompletionFlags(chunk.Payload)
				if comp {
					sawCompleted = true
				}
				if incomp {
					sawIncomplete = true
				}
			}
		}
	}
	if chunkErr != nil {
		rerrChunk := resultErrorFromError(chunkErr)
		if rerrChunk != nil {
			rerrChunk.Message = sanitizeRenewalMessage(rerrChunk.Message)
		}
		m.MarkResult(execCtx, Result{
			AuthID:            auth.ID,
			Provider:          provider,
			Model:             stateModel,
			RouteModel:        routeModel,
			Success:           false,
			Error:             rerrChunk,
			RetryAfter:        retryAfterFromError(chunkErr),
			CredentialScope:   isCredentialScopedError(chunkErr),
			Options:           opts,
			AccessToken:       accessToken,
			CredentialVersion: credVersion,
			RegistrationEpoch: regEpoch,
			ExecutionSeq:      executionSeq,
		})
		updated, _ := m.GetByID(auth.ID)
		return updated, chunkErr
	}
	if !sawCompleted || sawIncomplete {
		incompleteErr := &Error{Code: "incomplete_response", Message: "verification stream ended without a completed response", HTTPStatus: http.StatusBadGateway}
		updated, _ := m.GetByID(auth.ID)
		return updated, incompleteErr
	}
	m.MarkResult(execCtx, Result{
		AuthID:            auth.ID,
		Provider:          provider,
		Model:             stateModel,
		RouteModel:        routeModel,
		Success:           true,
		CompletedResponse: true,
		Options:           opts,
		AccessToken:       accessToken,
		CredentialVersion: credVersion,
		RegistrationEpoch: regEpoch,
		ExecutionSeq:      executionSeq,
	})
	updated, okUpdated := m.GetByID(auth.ID)
	if !okUpdated || updated == nil {
		return nil, &Error{Code: "auth_not_found", Message: "credential was removed during verification", HTTPStatus: http.StatusConflict}
	}
	if updated.CredentialVersion != credVersion || updated.RegistrationEpoch != regEpoch || !updated.InferenceVerified {
		return updated, &Error{Code: "credential_superseded", Message: "credential was replaced during verification; re-run verification for the new credential", HTTPStatus: http.StatusConflict}
	}
	return updated, nil
}
