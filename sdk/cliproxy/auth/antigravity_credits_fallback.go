package auth

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

func isSessionAffinityRetryError(err error) bool {
	if err == nil {
		return false
	}
	var authErr *Error
	return errors.As(err, &authErr) && authErr != nil && authErr.Code == "session_auth_retry"
}

func shouldReturnLastErrorOnPickFailure(lastErr error, errPick error) bool {
	return lastErr != nil && !isRequestStopError(errPick) && !isSessionAffinityRetryError(errPick)
}

func (m *Manager) localExecutionAllowed() bool {
	return m != nil
}

func (m *Manager) findAllAntigravityCreditsCandidateAuths(ctx context.Context, routeModel string, opts cliproxyexecutor.Options) ([]creditsCandidateEntry, error) {
	if m == nil || !m.localExecutionAllowed() {
		return nil, nil
	}
	pinnedAuthID := pinnedAuthIDFromMetadata(opts.Metadata)
	selectionModel := authSelectionModelFromOptions(opts, routeModel)
	var candidates []creditsCandidateEntry
	m.mu.RLock()
	if affinity, ok := m.selector.(*SessionAffinitySelector); ok && affinity != nil && affinity.rebindOnNever {
		if boundAuthID, okBound := affinity.boundAuthForRequest([]string{"antigravity"}, selectionModel, opts); okBound {
			if pinnedAuthID != "" && pinnedAuthID != boundAuthID {
				m.mu.RUnlock()
				return nil, nil
			}
			pinnedAuthID = boundAuthID
		}
	}
	for _, auth := range m.auths {
		if auth == nil || auth.Disabled || auth.Status == StatusDisabled {
			continue
		}
		if pinnedAuthID != "" && auth.ID != pinnedAuthID {
			continue
		}
		if !authProviderIs(auth, "antigravity") {
			continue
		}
		if !strings.Contains(strings.ToLower(strings.TrimSpace(routeModel)), "claude") {
			continue
		}
		providerKey := executorKeyFromAuth(auth)
		executor, ok := m.executorLocked(providerKey)
		if !ok {
			continue
		}
		candidates = append(candidates, creditsCandidateEntry{
			auth:     auth.Clone(),
			executor: executor,
			provider: providerKey,
		})
	}
	m.mu.RUnlock()

	now := time.Now()
	var known []creditsCandidateEntry
	var unknown []creditsCandidateEntry
	for _, candidate := range candidates {
		hint, okHint, errHint := GetAntigravityCreditsHintRequired(ctx, candidate.auth.ID)
		if errHint != nil {
			return nil, antigravityCreditsKVUnavailableError(errHint)
		}
		if okHint && hint.Known {
			if !hint.Available {
				if IsAntigravityCreditsHintFresh(hint, now) {
					continue
				}
				unknown = append(unknown, candidate)
				continue
			}
			known = append(known, candidate)
			continue
		}
		unknown = append(unknown, candidate)
	}
	sort.Slice(known, func(i, j int) bool {
		return known[i].auth.ID < known[j].auth.ID
	})
	sort.Slice(unknown, func(i, j int) bool {
		return unknown[i].auth.ID < unknown[j].auth.ID
	})
	return append(known, unknown...), nil
}

type creditsCandidateEntry struct {
	auth     *Auth
	executor ProviderExecutor
	provider string
}

func hasAntigravityProvider(providers []string) bool {
	for _, p := range providers {
		if strings.EqualFold(strings.TrimSpace(p), "antigravity") {
			return true
		}
	}
	return false
}

func shouldAttemptAntigravityCreditsFallback(m *Manager, lastErr error, providers []string) bool {
	if isRequestTerminatedError(lastErr) || isRequestStopError(lastErr) {
		return false
	}
	status := statusCodeFromError(lastErr)
	log.WithFields(log.Fields{
		"lastErr":   errorString(lastErr),
		"status":    status,
		"providers": providers,
	}).Debug("shouldAttemptAntigravityCreditsFallback")
	if m == nil || lastErr == nil {
		return false
	}
	cfg, _ := m.runtimeConfig.Load().(*internalconfig.Config)
	if cfg == nil || !cfg.QuotaExceeded.AntigravityCredits {
		return false
	}
	switch status {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	case 0:
		var authErr *Error
		if errors.As(lastErr, &authErr) && authErr != nil {
			return authErr.Code == "auth_not_found" || authErr.Code == "auth_unavailable" || authErr.Code == "model_cooldown"
		}
		var cooldownErr *modelCooldownError
		if errors.As(lastErr, &cooldownErr) {
			return true
		}
		return false
	default:
		return false
	}
}

func (m *Manager) tryAntigravityCreditsExecute(ctx context.Context, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, bool, error) {
	if !m.localExecutionAllowed() {
		return cliproxyexecutor.Response{}, false, nil
	}
	routeModel := req.Model
	candidates, errCandidates := m.findAllAntigravityCreditsCandidateAuths(ctx, routeModel, opts)
	if errCandidates != nil {
		return cliproxyexecutor.Response{}, false, errCandidates
	}
	for _, c := range candidates {
		if ctx.Err() != nil {
			return cliproxyexecutor.Response{}, false, nil
		}
		creditsCtx := WithAntigravityCredits(ctx)
		if rt := m.roundTripperFor(c.auth); rt != nil {
			creditsCtx = context.WithValue(creditsCtx, roundTripperContextKey{}, rt)
			creditsCtx = context.WithValue(creditsCtx, "cliproxy.roundtripper", rt)
		}
		creditsOpts := ensureRequestedModelMetadata(opts, routeModel)
		clearSchedulerSelectionID(creditsOpts.Metadata)
		creditsCtx = contextWithRequestedModelAlias(creditsCtx, creditsOpts, routeModel)
		creditsCtx = coreusage.WithSelectionID(creditsCtx, "")
		preparedAuth, errPrepare := m.prepareRequestAuth(creditsCtx, c.executor, c.auth)
		if errPrepare != nil {
			continue
		}
		c.auth = preparedAuth
		publishSelectedAuthMetadata(creditsOpts.Metadata, c.auth)
		models, pooled, aliasResult, routing := m.executionModelCandidatesWithAlias(c.auth, routeModel)
		if len(models) == 0 {
			continue
		}
		for _, upstreamModel := range models {
			resultModel := m.stateModelForExecution(c.auth, routeModel, upstreamModel, pooled)
			execReq := req
			execReq.Model = upstreamModel
			creditsCtx = syncMetadataSessionToContext(creditsCtx, creditsOpts.Metadata)
			executionSeq := m.nextExecutionSeq()
			resp, errExec := c.executor.Execute(creditsCtx, c.auth, execReq, creditsOpts)
			result := Result{AuthID: c.auth.ID, Provider: c.provider, Model: resultModel, RouteModel: routeModel, Success: errExec == nil, Options: creditsOpts, CredentialVersion: c.auth.CredentialVersion, RegistrationEpoch: c.auth.RegistrationEpoch, ExecutionSeq: executionSeq}
			if errExec != nil {
				result.Error = resultErrorFromError(errExec)
				if ra := retryAfterFromError(errExec); ra != nil {
					result.RetryAfter = ra
				}
				if isCredentialScopedError(errExec) {
					result.CredentialScope = true
				}
				m.MarkResult(creditsCtx, result)
				if result.CredentialScope {
					break
				}
				continue
			}
			m.MarkResult(creditsCtx, result)
			attemptAliasResult := resolveAttemptAliasResult(routing, c.auth, routeModel, upstreamModel, aliasResult)
			rewriteForceMappedResponse(&resp, attemptAliasResult)
			return resp, true, nil
		}
	}
	return cliproxyexecutor.Response{}, false, nil
}

func (m *Manager) tryAntigravityCreditsExecuteStream(ctx context.Context, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, bool, error) {
	if !m.localExecutionAllowed() {
		return nil, false, nil
	}
	routeModel := req.Model
	candidates, errCandidates := m.findAllAntigravityCreditsCandidateAuths(ctx, routeModel, opts)
	if errCandidates != nil {
		return nil, false, errCandidates
	}
	for _, c := range candidates {
		if ctx.Err() != nil {
			return nil, false, nil
		}
		creditsCtx := WithAntigravityCredits(ctx)
		if rt := m.roundTripperFor(c.auth); rt != nil {
			creditsCtx = context.WithValue(creditsCtx, roundTripperContextKey{}, rt)
			creditsCtx = context.WithValue(creditsCtx, "cliproxy.roundtripper", rt)
		}
		creditsOpts := ensureRequestedModelMetadata(opts, routeModel)
		clearSchedulerSelectionID(creditsOpts.Metadata)
		creditsCtx = contextWithRequestedModelAlias(creditsCtx, creditsOpts, routeModel)
		creditsCtx = coreusage.WithSelectionID(creditsCtx, "")
		preparedAuth, errPrepare := m.prepareRequestAuth(creditsCtx, c.executor, c.auth)
		if errPrepare != nil {
			continue
		}
		c.auth = preparedAuth
		publishSelectedAuthMetadata(creditsOpts.Metadata, c.auth)
		models, pooled, aliasResult, routing := m.executionModelCandidatesWithAlias(c.auth, routeModel)
		if len(models) == 0 {
			continue
		}
		creditsCtx = syncMetadataSessionToContext(creditsCtx, creditsOpts.Metadata)
		result, errStream := m.executeStreamWithModelPool(creditsCtx, c.executor, c.auth, c.provider, req, creditsOpts, routeModel, "", models, pooled, aliasResult, routing, true, false)
		if errStream != nil {
			continue
		}
		return result, true, nil
	}
	return nil, false, nil
}

func antigravityCreditsKVUnavailableError(cause error) error {
	if cause == nil {
		return &Error{Code: "home_kv_unavailable", Message: "home kv store unavailable", HTTPStatus: http.StatusServiceUnavailable}
	}
	return &Error{Code: "home_kv_unavailable", Message: "home kv store unavailable: " + cause.Error(), HTTPStatus: http.StatusServiceUnavailable}
}
