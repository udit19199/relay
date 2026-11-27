package helps

import (
	"context"

	"github.com/udit19199/relay/internal/logging"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	"github.com/udit19199/relay/internal/util"
)

// EnsureSessionContext ensures that ctx carries the internal session identity
// for $CPA-SESSION-ID expansion in custom headers.
func EnsureSessionContext(ctx context.Context, opts coreexecutor.Options, payload []byte) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id := util.SessionIDFromContext(ctx); id != "" {
		return ctx
	}
	if util.HasExplicitSessionID(ctx) {
		return ctx
	}
	if meta := logging.GetClientRequestMetadata(ctx); meta.SessionID != "" {
		return util.WithSessionID(ctx, meta.SessionID)
	}
	evalPayload := opts.OriginalRequest
	if len(evalPayload) == 0 {
		evalPayload = payload
	}
	canonical := coreauth.CanonicalSessionID(opts.Headers, evalPayload, opts.Metadata)
	if canonical != "" {
		return util.WithSessionID(ctx, canonical)
	}
	return ctx
}
