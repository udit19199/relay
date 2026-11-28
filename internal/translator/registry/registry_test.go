package registry

import (
	"context"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/udit19199/relay/internal/registry"
)

func TestTranslateRequest_FallbackNormalizesModel(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name          string
		model         string
		payload       string
		wantModel     string
		wantUnchanged bool
	}{
		{
			name:      "prefixed model is rewritten",
			model:     "gpt-5-mini",
			payload:   `{"model":"copilot/gpt-5-mini","input":"ping"}`,
			wantModel: "gpt-5-mini",
		},
		{
			name:          "matching model is left unchanged",
			model:         "gpt-5-mini",
			payload:       `{"model":"gpt-5-mini","input":"ping"}`,
			wantModel:     "gpt-5-mini",
			wantUnchanged: true,
		},
		{
			name:          "empty model leaves payload unchanged",
			model:         "",
			payload:       `{"model":"copilot/gpt-5-mini","input":"ping"}`,
			wantModel:     "copilot/gpt-5-mini",
			wantUnchanged: true,
		},
		{
			name:      "deeply prefixed model is rewritten",
			model:     "gpt-5.3-codex",
			payload:   `{"model":"team/gpt-5.3-codex","stream":true}`,
			wantModel: "gpt-5.3-codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.payload)
			got := r.TranslateRequest(Format("a"), Format("b"), tt.model, input, false)

			gotModel := ojson.GetBytesNoCopy(got, "model").String()
			if gotModel != tt.wantModel {
				t.Errorf("model = %q, want %q", gotModel, tt.wantModel)
			}

			if tt.wantUnchanged && string(got) != tt.payload {
				t.Errorf("payload was modified when it should not have been:\ngot:  %s\nwant: %s", got, tt.payload)
			}

			// Verify other fields are preserved.
			for _, key := range []string{"input", "stream"} {
				orig := ojson.Get(tt.payload, key)
				if !orig.Exists() {
					continue
				}
				after := ojson.GetBytesNoCopy(got, key)
				if orig.Raw != after.Raw {
					t.Errorf("field %q changed: got %s, want %s", key, after.Raw, orig.Raw)
				}
			}
		})
	}
}

func TestTranslateRequest_RegisteredTransformTakesPrecedence(t *testing.T) {
	r := NewRegistry()
	from := Format("openai-response")
	to := Format("openai-response")

	r.Register(from, to, func(model string, rawJSON []byte, stream bool) ([]byte, error) {
		return []byte(`{"model":"from-transform"}`), nil
	}, ResponseTransform{})

	input := []byte(`{"model":"copilot/gpt-5-mini","input":"ping"}`)
	got := r.TranslateRequest(from, to, "gpt-5-mini", input, false)

	gotModel := ojson.GetBytesNoCopy(got, "model").String()
	if gotModel != "from-transform" {
		t.Errorf("expected registered transform to take precedence, got model = %q", gotModel)
	}
}

func TestHasRequestTransformer(t *testing.T) {
	r := NewRegistry()
	from := Format("from")
	to := Format("to")

	if r.HasRequestTransformer(from, to) {
		t.Fatal("request transformer exists before registration")
	}

	r.Register(from, to, func(model string, rawJSON []byte, stream bool) ([]byte, error) {
		return rawJSON, nil
	}, ResponseTransform{})

	if !r.HasRequestTransformer(from, to) {
		t.Fatal("request transformer is missing after registration")
	}
}

func TestHasResponseTransformerIgnoresEmptyRegistration(t *testing.T) {
	r := NewRegistry()
	from := Format("from")
	to := Format("to")

	r.Register(from, to, func(model string, rawJSON []byte, stream bool) ([]byte, error) {
		return rawJSON, nil
	}, ResponseTransform{})

	if r.HasResponseTransformer(from, to) {
		t.Fatal("empty response transform was reported as a response transformer")
	}
	if r.HasStreamResponseTransformer(from, to) {
		t.Fatal("empty response transform was reported as a stream response transformer")
	}
	if r.HasNonStreamResponseTransformer(from, to) {
		t.Fatal("empty response transform was reported as a non-stream response transformer")
	}
}

func TestHasResponseTransformerChecksConcreteResponseKinds(t *testing.T) {
	ctx := context.Background()
	r := NewRegistry()
	from := Format("from")
	streamOnlyTo := Format("stream-to")
	nonStreamOnlyTo := Format("non-stream-to")

	r.Register(from, streamOnlyTo, nil, ResponseTransform{
		Stream: func(ctx context.Context, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
			return [][]byte{rawJSON}
		},
	})
	r.Register(from, nonStreamOnlyTo, nil, ResponseTransform{
		NonStream: func(ctx context.Context, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
			return rawJSON
		},
	})

	if !r.HasResponseTransformer(from, streamOnlyTo) {
		t.Fatal("stream response transform was not reported as a response transformer")
	}
	if !r.HasStreamResponseTransformer(from, streamOnlyTo) {
		t.Fatal("stream response transform was not reported as a stream response transformer")
	}
	if r.HasNonStreamResponseTransformer(from, streamOnlyTo) {
		t.Fatal("stream-only transform was reported as a non-stream response transformer")
	}

	if !r.HasResponseTransformer(from, nonStreamOnlyTo) {
		t.Fatal("non-stream response transform was not reported as a response transformer")
	}
	if r.HasStreamResponseTransformer(from, nonStreamOnlyTo) {
		t.Fatal("non-stream-only transform was reported as a stream response transformer")
	}
	if !r.HasNonStreamResponseTransformer(from, nonStreamOnlyTo) {
		t.Fatal("non-stream response transform was not reported as a non-stream response transformer")
	}

	got := r.TranslateStream(ctx, streamOnlyTo, from, "model", nil, nil, []byte(`data: {"ok":true}`), nil)
	if len(got) != 1 || string(got[0]) != `data: {"ok":true}` {
		t.Fatalf("stream transform output = %q", got)
	}
}

func TestTranslateStream_NativeEmptyOutputSuppressesRawFallback(t *testing.T) {
	ctx := context.Background()
	from := Format("client")
	to := Format("upstream")

	r := NewRegistry()
	r.Register(to, from, nil, ResponseTransform{
		Stream: func(ctx context.Context, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
			return nil
		},
	})

	got := r.TranslateStream(ctx, from, to, "model", nil, nil, []byte(`data: {"raw":true}`), nil)
	if len(got) != 0 {
		t.Fatalf("native stream transformer returned empty output, got raw fallback %q", got)
	}
}

func TestRequestEnvelopePreservesRegisteredTransformDispatch(t *testing.T) {
	r := NewRegistry()
	from := FormatOpenAIResponse
	to := FormatClaude
	modelInfo := &registry.ModelInfo{ID: "home-model"}

	r.RegisterRequestEnvelope(from, to, func(_ context.Context, req RequestEnvelope) RequestEnvelope {
		if req.ModelInfo == nil {
			req.Body = []byte(`{"source":"native"}`)
		} else {
			req.Body = []byte(`{"source":"model-info"}`)
		}
		return req
	})

	withoutMetadata := r.TranslateRequest(from, to, "home-model", []byte(`{"input":"hello"}`), false)
	if ojson.GetBytesNoCopy(withoutMetadata, "source").String() != "native" {
		t.Fatalf("without metadata used unexpected transform: %s", withoutMetadata)
	}
	withMetadata, err := NewPipeline(r).TranslateRequest(context.Background(), from, to, RequestEnvelope{
		Format: from, Model: "home-model", Body: []byte(`{"input":"hello"}`), ModelInfo: modelInfo,
	})
	if err != nil {
		t.Fatalf("pipeline translation failed: %v", err)
	}
	if ojson.GetBytesNoCopy(withMetadata.Body, "source").String() != "model-info" {
		t.Fatalf("envelope metadata was not used: %s", withMetadata.Body)
	}
	if withMetadata.ModelInfo != modelInfo {
		t.Fatal("pipeline did not preserve request-scoped model info")
	}

	// A custom registration replaces the envelope-aware native route.
	r.Register(from, to, func(string, []byte, bool) ([]byte, error) {
		return []byte(`{"source":"custom"}`), nil
	}, ResponseTransform{})
	for _, payload := range [][]byte{
		[]byte(`{"input":"hello"}`),
		[]byte(`{"input":"weather","tools":[{"type":"web_search"}]}`),
	} {
		customWithMetadata := r.TranslateRequestEnvelope(context.Background(), from, to, RequestEnvelope{
			Format: from, Model: "home-model", Body: payload, ModelInfo: modelInfo,
		})
		if ojson.GetBytesNoCopy(customWithMetadata.Body, "source").String() != "custom" {
			t.Fatalf("request metadata bypassed custom transform for %s: %s", payload, customWithMetadata.Body)
		}
	}
}

func TestUnregisterRestoresFormatPair(t *testing.T) {
	from := Format("unregister-default-from")
	to := Format("unregister-default-to")
	other := Format("unregister-default-other")
	if HasRequestTransformer(from, to) || HasRequestTransformer(from, other) {
		t.Fatal("test formats are already registered")
	}
	identity := func(_ string, rawJSON []byte, _ bool) ([]byte, error) {
		return append([]byte(nil), rawJSON...), nil
	}
	Register(from, to, identity, ResponseTransform{
		NonStream: func(context.Context, string, []byte, []byte, []byte, *any) []byte {
			return []byte(`{"removed":true}`)
		},
	})
	Register(from, other, identity, ResponseTransform{})
	t.Cleanup(func() {
		Unregister(from, to)
		Unregister(from, other)
	})

	if !HasRequestTransformer(from, to) || !HasNonStreamResponseTransformer(from, to) || !HasRequestTransformer(from, other) {
		t.Fatal("register did not store transforms")
	}
	Unregister(from, to)
	if HasRequestTransformer(from, to) || HasNonStreamResponseTransformer(from, to) || HasResponseTransformer(from, to) {
		t.Fatal("unregister left transforms behind")
	}
	if !HasRequestTransformer(from, other) {
		t.Fatal("unregister removed a different format pair")
	}
}
