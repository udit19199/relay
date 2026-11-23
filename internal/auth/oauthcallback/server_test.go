package oauthcallback

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleCallbackCapturesClientID(t *testing.T) {
	srv := New(0, Config{CallbackPath: "/auth/callback"})
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c1&state=s1&client_id=oaiapp_srv_1", nil)
	rec := httptest.NewRecorder()
	srv.handleCallback(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/success" {
		t.Fatalf("response = %d %q, want 302 /success", rec.Code, rec.Header().Get("Location"))
	}
	res, errWait := srv.WaitForCallback(time.Second)
	if errWait != nil {
		t.Fatalf("WaitForCallback error: %v", errWait)
	}
	if res.Code != "c1" || res.State != "s1" || res.ClientID != "oaiapp_srv_1" || res.Error != "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestHandleCallbackErrors(t *testing.T) {
	cases := map[string]string{
		"/cb?error=access_denied": "access_denied",
		"/cb?state=s1":            "no_code",
		"/cb?code=c1":             "no_state",
	}
	for target, want := range cases {
		srv := New(0, Config{CallbackPath: "/cb"})
		rec := httptest.NewRecorder()
		srv.handleCallback(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
		}
		res, errWait := srv.WaitForCallback(time.Second)
		if errWait != nil || res.Error != want {
			t.Fatalf("%s: result = %+v, err = %v, want error %q", target, res, errWait, want)
		}
	}
}

func TestHandleSuccessUsesDefaultPlatformURLAndSetupNotice(t *testing.T) {
	srv := New(0, Config{
		DefaultPlatformURL: "https://default.example/",
		SuccessHTML:        "<a href=\"{{PLATFORM_URL}}\"></a>{{SETUP_NOTICE}}",
		SetupNoticeHTML:    "<p>{{PLATFORM_URL}}</p>",
	})

	rec := httptest.NewRecorder()
	srv.handleSuccess(rec, httptest.NewRequest(http.MethodGet, "/success", nil))
	if got := rec.Body.String(); got != "<a href=\"https://default.example/\"></a>" {
		t.Fatalf("body = %q", got)
	}

	rec = httptest.NewRecorder()
	srv.handleSuccess(rec, httptest.NewRequest(http.MethodGet, "/success?setup_required=true&platform_url=https://custom.example", nil))
	if got := rec.Body.String(); !strings.Contains(got, "<p>https://custom.example</p>") || strings.Contains(got, "{{") {
		t.Fatalf("body = %q", got)
	}
}
