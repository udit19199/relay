package executor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	translatorcommon "github.com/udit19199/relay/internal/translator/common"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

const devinInteractionsHistory = `{"type":"user_input","content":[{"type":"text","text":"a"}]},{"type":"model_output","content":[{"type":"text","text":"b"}]}`

func devinInteractionsPayload(steps string) string {
	return `{"input":[` + devinInteractionsHistory + `,` + steps + `]}`
}

func requireDevinUnsupportedPart(t *testing.T, err error, wantType string) {
	t.Helper()
	var unsupported *translatorcommon.UnsupportedPartError
	if !errors.As(err, &unsupported) || unsupported.Type != wantType || unsupported.StatusCode() != 400 {
		t.Fatalf("err = %v, want unsupported content part: %s", err, wantType)
	}
	if got, want := err.Error(), "unsupported content part: "+wantType; got != want {
		t.Fatalf("error text = %q, want %q", got, want)
	}
}

// Devin cannot fetch a remote uri or take audio, video or documents, so a user
// turn that only holds such media is refused before any HTTP call, for both
// execution paths and for Interactions and non-Interactions sources.
func TestDevinExecutorRefusesAnUnsendableMediaOnlyTurnBeforeCallingUpstream(t *testing.T) {
	var upstreamCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const claudeWhitespaceImage = `{"max_tokens":8,"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":[{"type":"text","text":"  "},{"type":"image","source":{"type":"file","file_id":"f1"}}]}]}`
	cases := []struct {
		name     string
		source   translatorreg.Format
		payload  string
		wantPart string
	}{
		{"image by uri", translatorreg.FormatInteractions, devinInteractionsPayload(`{"type":"user_input","content":[{"type":"image","mime_type":"image/png","uri":"https://example.test/a.png"}]}`), "image"},
		{"image by file_uri", translatorreg.FormatInteractions, devinInteractionsPayload(`{"type":"user_input","content":[{"type":"image","mime_type":"image/png","file_uri":"gs://b/a.png"}]}`), "image"},
		{"document by fileUri", translatorreg.FormatInteractions, devinInteractionsPayload(`{"type":"user_input","content":[{"type":"document","mime_type":"application/pdf","fileUri":"gs://b/a.pdf"}]}`), "document"},
		{"inline audio", translatorreg.FormatInteractions, devinInteractionsPayload(`{"type":"user_input","content":[{"type":"audio","mime_type":"audio/wav","data":"UklGRg=="}]}`), "audio"},
		{"whitespace text does not hide it", translatorreg.FormatInteractions, devinInteractionsPayload(`{"type":"user_input","content":[{"type":"text","text":" \n"},{"type":"video","mime_type":"video/mp4","uri":"https://example.test/a.mp4"}]}`), "video"},
		{"emptied turn before a later text turn", translatorreg.FormatInteractions, `{"input":[{"type":"user_input","content":[{"type":"image","mime_type":"image/png","uri":"https://example.test/a.png"}]},{"type":"model_output","content":[{"type":"text","text":"ok"}]},{"type":"user_input","content":[{"type":"text","text":"next"}]}]}`, "image"},
		{"claude whitespace text beside an unrepresentable image", translatorreg.FormatClaude, claudeWhitespaceImage, "image"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := NewDevinExecutor(&config.Config{})
			auth := &coreauth.Auth{ID: "devin-media-only", Provider: "devin", Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL}}
			req := coreexecutor.Request{Model: "devin/swe-2", Payload: []byte(tc.payload)}
			opts := coreexecutor.Options{SourceFormat: tc.source}

			_, errExecute := exec.Execute(context.Background(), auth, req, opts)
			requireDevinUnsupportedPart(t, errExecute, tc.wantPart)
			opts.Stream = true
			_, errStream := exec.ExecuteStream(context.Background(), auth, req, opts)
			requireDevinUnsupportedPart(t, errStream, tc.wantPart)
			_, _, _, errPrepare := exec.prepareDevinHTTPRequest(context.Background(), auth, req, opts)
			requireDevinUnsupportedPart(t, errPrepare, tc.wantPart)
		})
	}
	if got := upstreamCalls.Load(); got != 0 {
		t.Fatalf("upstream was called %d times, want 0", got)
	}
}

func TestDevinExecutorSendsTextBesideUnsendableMediaWithoutAPlaceholder(t *testing.T) {
	exec := NewDevinExecutor(&config.Config{})
	auth := &coreauth.Auth{ID: "devin-text-beside-media", Provider: "devin", Attributes: map[string]string{"api_key": "test-key"}}
	opts := coreexecutor.Options{SourceFormat: translatorreg.FormatInteractions}
	cases := map[string]string{
		"same step":         `{"type":"user_input","content":[{"type":"text","text":"keep me"},{"type":"image","mime_type":"image/png","uri":"https://example.test/a.png"}]}`,
		"neighbouring step": `{"type":"user_input","content":[{"type":"text","text":"keep me"}]},{"type":"user_input","content":[{"type":"image","mime_type":"image/png","uri":"https://example.test/a.png"}]}`,
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			req := coreexecutor.Request{Model: "devin/swe-2", Payload: []byte(devinInteractionsPayload(steps))}
			httpReq, _, _, err := exec.prepareDevinHTTPRequest(context.Background(), auth, req, opts)
			if err != nil {
				t.Fatalf("prepareDevinHTTPRequest: %v", err)
			}
			body, errRead := io.ReadAll(httpReq.Body)
			if errRead != nil {
				t.Fatalf("read body: %v", errRead)
			}
			if !bytes.Contains(body, []byte("keep me")) {
				t.Fatalf("text was lost from the request")
			}
			for _, forbidden := range []string{"example.test", "omitted", "[Image"} {
				if bytes.Contains(body, []byte(forbidden)) {
					t.Fatalf("request mentions %q although the media was not sent", forbidden)
				}
			}
		})
	}
}

func TestDevinExecutorStillSendsInlineImages(t *testing.T) {
	exec := NewDevinExecutor(&config.Config{})
	auth := &coreauth.Auth{ID: "devin-inline-image", Provider: "devin", Attributes: map[string]string{"api_key": "test-key"}}
	opts := coreexecutor.Options{SourceFormat: translatorreg.FormatInteractions}
	for _, part := range []string{
		`{"type":"image","mime_type":"image/png","data":"aGVsbG8="}`,
		`{"type":"image","mime_type":"image/png","uri":"data:image/png;base64,aGVsbG8="}`,
	} {
		req := coreexecutor.Request{Model: "devin/swe-2", Payload: []byte(devinInteractionsPayload(`{"type":"user_input","content":[` + part + `]}`))}
		httpReq, _, _, err := exec.prepareDevinHTTPRequest(context.Background(), auth, req, opts)
		if err != nil {
			t.Fatalf("prepareDevinHTTPRequest(%s): %v", part, err)
		}
		body, errRead := io.ReadAll(httpReq.Body)
		if errRead != nil {
			t.Fatalf("read body: %v", errRead)
		}
		if !bytes.Contains(body, []byte("pasted_image_1.png")) {
			t.Fatalf("inline image was not sent for %s", part)
		}
	}
}
