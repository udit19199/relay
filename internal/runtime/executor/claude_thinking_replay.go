package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/MoeForge/moego/ojson"
	log "github.com/sirupsen/logrus"
	internalcache "github.com/udit19199/relay/internal/cache"
	"github.com/udit19199/relay/internal/runtime/executor/helps"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	"github.com/udit19199/relay/internal/thinking"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

// claudeThinkingReplayScope reuses the bounded replay state shape shared by replay-capable Claude executors.
type claudeThinkingReplayScope = thinkingReplayScope

func claudeThinkingReplayEnabled(auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) bool {
	if auth == nil || !sourceFormatEqual(opts.SourceFormat, translatorreg.FormatClaude) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Provider), "claude") || auth.AuthKind() != coreauth.AuthKindAPIKey {
		return false
	}
	if !helps.APIKeyModelIsCompat(req) {
		return false
	}
	apiKey, _ := claudeCreds(auth)
	return strings.TrimSpace(apiKey) != "" && !isClaudeOAuthToken(apiKey)
}

// A missing session identity intentionally disables replay instead of sharing hidden reasoning across callers.
func claudeThinkingReplayScopeFromRequest(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) claudeThinkingReplayScope {
	sessionKey := codexReasoningReplaySessionKey(ctx, translatorreg.FormatClaude, req, opts, req.Payload)
	sessionKey = xaiReasoningReplayIsolateSessionKey(ctx, sessionKey)
	return claudeThinkingReplayScope{
		modelFamily: claudeThinkingReplayModelFamily(auth, req.Model),
		sessionKey:  sessionKey,
	}
}

func claudeThinkingReplayModelFamily(auth *coreauth.Auth, model string) string {
	baseModel := thinking.ParseSuffix(strings.TrimSpace(model)).ModelName
	if baseModel == "" {
		return ""
	}
	identity := ""
	if auth != nil {
		identity = strings.TrimSpace(auth.ID)
		if identity == "" {
			apiKey, baseURL := claudeCreds(auth)
			identity = strings.TrimSpace(baseURL)
			if identity == "" {
				identity = strings.TrimSpace(apiKey)
			}
		}
	}
	if identity == "" {
		return "claude:" + baseModel
	}
	sum := sha256.Sum256([]byte(identity))
	return "claude:" + hex.EncodeToString(sum[:8]) + ":" + baseModel
}

func prepareClaudeThinkingReplayRequest(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Request, claudeThinkingReplayScope) {
	scope := claudeThinkingReplayScopeFromRequest(ctx, auth, req, opts)
	if !scope.valid() {
		return req, scope
	}
	contents, snapshot, found, errGet := internalcache.GetClaudeThinkingReplayWithSnapshotRequired(ctx, scope.modelFamily, scope.sessionKey)
	scope.snapshot = snapshot
	scope.cacheReady = errGet == nil
	if errGet != nil {
		log.Warnf("claude compatible thinking replay cache read failed: %v", errGet)
		return req, scope
	}
	if !found {
		return req, scope
	}
	updated, restored := restoreClaudeThinkingReplayContents(req.Payload, contents)
	if restored {
		req.Payload = updated
		scope.replayApplied = true
	}
	return req, scope
}

func restoreClaudeThinkingReplayContents(body []byte, cachedContents [][]byte) ([]byte, bool) {
	if len(cachedContents) == 0 {
		return body, false
	}
	doc := ojson.NewDoc(body)
	restored := false
	for _, cachedContent := range cachedContents {
		if restoreThinkingReplayContentDoc(doc, cachedContent) {
			restored = true
		}
	}
	if !restored {
		return body, false
	}
	return doc.Bytes(), true
}

func cacheClaudeThinkingReplayResponse(ctx context.Context, scope claudeThinkingReplayScope, response []byte) {
	content := ojson.GetBytesNoCopy(response, "content")
	if content.IsArray() {
		cacheClaudeThinkingReplayContent(ctx, scope, []byte(content.Raw))
		return
	}
	accumulator := newThinkingReplayStreamAccumulator()
	accumulator.observe(response)
	if content, completed := accumulator.content(); completed {
		cacheClaudeThinkingReplayContent(ctx, scope, content)
	}
}

func cacheClaudeThinkingReplayContent(ctx context.Context, scope claudeThinkingReplayScope, content []byte) {
	if !scope.valid() || !scope.cacheReady {
		return
	}
	if thinkingReplayContentIsReplayable(content) {
		if _, errReplace := internalcache.ReplaceClaudeThinkingReplayIfUnchanged(ctx, scope.modelFamily, scope.sessionKey, scope.snapshot, content); errReplace != nil {
			log.Warnf("claude compatible thinking replay cache replace failed: %v", errReplace)
		}
		return
	}
	clearClaudeThinkingReplayContent(ctx, scope)
}

func clearClaudeThinkingReplayContent(ctx context.Context, scope claudeThinkingReplayScope) {
	if !scope.valid() || !scope.cacheReady {
		return
	}
	if _, errDelete := internalcache.DeleteClaudeThinkingReplayIfUnchanged(ctx, scope.modelFamily, scope.sessionKey, scope.snapshot); errDelete != nil {
		log.Warnf("claude compatible thinking replay cache delete failed: %v", errDelete)
	}
}

func wrapClaudeThinkingReplayStream(ctx context.Context, result *coreexecutor.StreamResult, scope claudeThinkingReplayScope) *coreexecutor.StreamResult {
	return wrapThinkingReplayStream(ctx, result, scope, cacheClaudeThinkingReplayContent, clearClaudeThinkingReplayContent)
}
