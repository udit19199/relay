package auth_test

import (
	"net/http"
	"testing"

	coreauth "github.com/udit19199/relay/internal/service/auth"
)

func TestErrorLegacyUnkeyedLiteralCompatibility(t *testing.T) {
	err := coreauth.Error{"code", "message", false, http.StatusRequestTimeout}

	if err.Code != "code" || err.Message != "message" || err.Retryable || err.HTTPStatus != http.StatusRequestTimeout {
		t.Fatalf("unexpected error fields: %#v", err)
	}
}
