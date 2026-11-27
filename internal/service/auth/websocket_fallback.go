package auth

import (
	"context"

	"github.com/MoeForge/moego/ojson"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

func sanitizeDownstreamWebsocketFallbackRequest(ctx context.Context, auth *Auth, req coreexecutor.Request) coreexecutor.Request {
	if !coreexecutor.DownstreamWebsocket(ctx) || authWebsocketsEnabled(auth) || len(req.Payload) == 0 {
		return req
	}
	updated, errDelete := ojson.DeleteBytes(req.Payload, "generate")
	if errDelete != nil {
		return req
	}
	req.Payload = updated
	return req
}
