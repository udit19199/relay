package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gorilla/websocket"
	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
	core "github.com/udit19199/relay/internal/service/executor"
	translatorreg "github.com/udit19199/relay/internal/translator/registry"
)

// The upstream deliberately cannot finish the initial response until it reads
// steering. A serial request/response proxy deadlocks this test.

func TestCodexDuplexSIWCSteerStripsConfigurationUpdates(t *testing.T) {
	captured := make(chan []byte, 1)
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		conn, errUpgrade := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if errUpgrade != nil {
			t.Errorf("upgrade websocket: %v", errUpgrade)
			return
		}
		defer func() { _ = conn.Close() }()
		// This deadline bounds test failures only.
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, first, errRead := conn.ReadMessage()
		if errRead != nil {
			t.Errorf("read initial create: %v", errRead)
			return
		}
		if ojson.GetBytes(first, "type").String() != "response.create" {
			t.Errorf("initial create: %s", first)
		}
		if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"r1","output":[]}}`)); errWrite != nil {
			t.Errorf("write response.created: %v", errWrite)
			return
		}
		_, steer, errSteer := conn.ReadMessage()
		if errSteer != nil {
			t.Errorf("read steer: %v", errSteer)
			return
		}
		captured <- steer
		for _, event := range []string{
			`{"type":"response.steer.accepted","steer":{"id":"s1","previous_response_id":"r1"}}`,
			`{"type":"response.completed","response":{"id":"r1","output":[]}}`,
		} {
			if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(event)); errWrite != nil {
				t.Errorf("write event: %v", errWrite)
				return
			}
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	input := make(chan core.WebsocketInput, 1)
	ctx = core.WithWebsocketInput(core.WithDownstreamWebsocket(ctx), input)
	exec := NewCodexWebsocketsExecutor(&config.Config{Codex: config.CodexConfig{ResponseSteering: true}})
	exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}
	req := core.Request{
		Model:   "gpt-6-astra",
		Payload: []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"instructions":"Initial settings"}`),
	}
	opts := core.Options{SourceFormat: translatorreg.FromString("codex"), Metadata: map[string]any{core.ExecutionSessionMetadataKey: "duplex-siwc-steer"}}
	result, err := exec.ExecuteStream(ctx, codexSIWCAuthForUpdateTest(server.URL), req, opts)
	if err != nil {
		t.Fatal(err)
	}

	completed := false
	for !completed {
		select {
		case chunk, ok := <-result.Chunks:
			if !ok {
				t.Fatal("stream ended before response.completed")
			}
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
			event := ojson.GetBytes(chunk.Payload, "type").String()
			id := ojson.GetBytes(chunk.Payload, "response.id").String()
			if event == "response.created" && id == "r1" {
				input <- core.WebsocketInput{Payload: []byte(`{"type":"response.steer","previous_response_id":"r1","input":[{"type":"configuration_update","reasoning":{"effort":"high"}},{"role":"user","content":[{"type":"input_text","text":"STEER_OK"}]}]}`)}
			}
			completed = event == "response.completed" && id == "r1"
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	cancel()
	for range result.Chunks {
	}
	select {
	case <-serverDone:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not close")
	}

	select {
	case got := <-captured:
		if ojson.GetBytes(got, "type").String() != "response.steer" || ojson.GetBytes(got, "previous_response_id").String() != "r1" {
			t.Fatalf("steer routing changed: %s", got)
		}
		items := ojson.GetBytes(got, "input").Array()
		if len(items) != 1 || items[0].Get("role").String() != "user" || items[0].Get("content.0.text").String() != "STEER_OK" {
			t.Fatalf("steer input must contain only the user message: %s", got)
		}
		if ojson.GetBytes(got, "reasoning").Exists() {
			t.Fatalf("steer must not promote reasoning: %s", got)
		}
		if ojson.GetBytes(got, "store").Exists() || ojson.GetBytes(got, "stream").Exists() {
			t.Fatalf("steer must not inherit response.create defaults: %s", got)
		}
	default:
		t.Fatal("upstream steer was not captured")
	}
}

func TestCodexDuplexAppendInheritsContextAndInstructions(t *testing.T) {
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		// 1. Read initial create
		_, first, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read first create: %v", err)
			return
		}
		if ojson.GetBytes(first, "instructions").String() != "Initial system instructions" {
			t.Errorf("first instructions = %q", ojson.GetBytes(first, "instructions").String())
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp-1","output":[]}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-1","output":[]}}`))

		// 2. Read subsequent append (which client sent omitting previous_response_id, model, and instructions)
		_, second, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read second create (from append): %v", err)
			return
		}
		if got := ojson.GetBytes(second, "type").String(); got != "response.create" {
			t.Errorf("second type = %q, want response.create", got)
		}
		if got := ojson.GetBytes(second, "previous_response_id").String(); got != "resp-1" {
			t.Errorf("second previous_response_id = %q, want resp-1", got)
		}
		if got := ojson.GetBytes(second, "model").String(); got != "gpt-6-astra" {
			t.Errorf("second model = %q, want gpt-6-astra", got)
		}
		if got := ojson.GetBytes(second, "instructions").String(); got != "Initial system instructions" {
			t.Errorf("second instructions = %q, want 'Initial system instructions'", got)
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp-2","output":[]}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-2","output":[]}}`))
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := make(chan core.WebsocketInput, 4)
	ctx = core.WithWebsocketInput(core.WithDownstreamWebsocket(ctx), input)

	exec := NewCodexWebsocketsExecutor(&config.Config{Codex: config.CodexConfig{ResponseSteering: true}})
	exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}
	auth := &coreauth.Auth{ID: "append-test-auth", Provider: "codex", Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL, "websockets": "true"}}
	req := core.Request{
		Model:   "gpt-6-astra",
		Payload: []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"turn 1"}],"instructions":"Initial system instructions"}`),
	}
	opts := core.Options{SourceFormat: translatorreg.FromString("codex"), Metadata: map[string]any{core.ExecutionSessionMetadataKey: "append-test"}}

	result, err := exec.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatal(err)
	}

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		eventType := ojson.GetBytes(chunk.Payload, "type").String()
		eventID := ojson.GetBytes(chunk.Payload, "response.id").String()
		if eventType == "response.completed" && eventID == "resp-1" {
			// Client sends response.append omitting previous_response_id, model, instructions
			appendPayload := []byte(`{"type":"response.append","input":[{"role":"user","content":"turn 2"}]}`)
			input <- core.WebsocketInput{Payload: appendPayload}
		}
		if eventType == "response.completed" && eventID == "resp-2" {
			cancel()
			break
		}
	}

	select {
	case <-serverDone:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not finish")
	}
}

func TestCodexDuplexStandaloneCreateDoesNotInheritParentID(t *testing.T) {
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		// 1. Read first create
		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Errorf("read first create: %v", err)
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp-1","output":[]}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-1","output":[]}}`))

		// 2. Read second create (standalone / full history replacement without previous_response_id)
		_, second, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read second create: %v", err)
			return
		}
		if got := ojson.GetBytes(second, "type").String(); got != "response.create" {
			t.Errorf("second type = %q, want response.create", got)
		}
		if ojson.GetBytes(second, "previous_response_id").Exists() {
			t.Errorf("standalone create must not gain previous_response_id: %s", second)
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp-2","output":[]}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-2","output":[]}}`))
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := make(chan core.WebsocketInput, 4)
	ctx = core.WithWebsocketInput(core.WithDownstreamWebsocket(ctx), input)

	exec := NewCodexWebsocketsExecutor(&config.Config{Codex: config.CodexConfig{ResponseSteering: true}})
	exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}
	auth := &coreauth.Auth{ID: "standalone-create-test", Provider: "codex", Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL, "websockets": "true"}}
	req := core.Request{
		Model:   "gpt-6-astra",
		Payload: []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"turn 1"}]}`),
	}
	opts := core.Options{SourceFormat: translatorreg.FromString("codex"), Metadata: map[string]any{core.ExecutionSessionMetadataKey: "standalone-test"}}

	result, err := exec.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatal(err)
	}

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		eventType := ojson.GetBytes(chunk.Payload, "type").String()
		eventID := ojson.GetBytes(chunk.Payload, "response.id").String()
		if eventType == "response.completed" && eventID == "resp-1" {
			// Client sends standalone response.create without previous_response_id
			standalonePayload := []byte(`{"type":"response.create","model":"gpt-6-astra","input":[{"role":"user","content":"turn 2 standalone"}]}`)
			input <- core.WebsocketInput{Payload: standalonePayload}
		}
		if eventType == "response.completed" && eventID == "resp-2" {
			cancel()
			break
		}
	}

	select {
	case <-serverDone:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not finish")
	}
}

func TestCodexDuplexQueuedCreateDoesNotBlockSubsequentSteer(t *testing.T) {
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		// 1. Read first create
		_, first, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read first create: %v", err)
			return
		}
		if ojson.GetBytes(first, "type").String() != "response.create" {
			t.Errorf("expected response.create: %s", first)
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"r1","output":[]}}`))

		// Read first steer for r1
		_, steer1, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read steer1: %v", err)
			return
		}
		if ojson.GetBytes(steer1, "type").String() != "response.steer" {
			t.Errorf("expected response.steer: %s", steer1)
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.steer.accepted","steer":{"id":"s1","previous_response_id":"r1"}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.incomplete","response":{"id":"r1","output":[],"incomplete_details":{"reason":"steered"}}}`))

		// Emit automatic successor (sets automaticActive = true)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"auto-1","previous_response_id":"r1","output":[]}}`))

		// Client will send next create (blocked by automaticActive) followed by steer for auto-1.
		// Upstream expects steering to arrive NEXT, proving create did not block the steer!
		_, steerFrame, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read steer: %v", err)
			return
		}
		if got := ojson.GetBytes(steerFrame, "type").String(); got != "response.steer" {
			t.Errorf("expected response.steer to arrive before blocked create, got: %s", steerFrame)
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.steer.accepted","steer":{"id":"s2","previous_response_id":"auto-1"}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"auto-1","output":[]}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"auto-2","previous_response_id":"auto-1","output":[]}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"auto-2","output":[]}}`))

		// Now that automatic successor is completed, the queued create is delivered to upstream
		_, nextCreate, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read queued create: %v", err)
			return
		}
		if got := ojson.GetBytes(nextCreate, "type").String(); got != "response.create" {
			t.Errorf("queued create type = %q, want response.create", got)
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"r2","output":[]}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"r2","output":[]}}`))
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := make(chan core.WebsocketInput, 8)
	ctx = core.WithWebsocketInput(core.WithDownstreamWebsocket(ctx), input)

	exec := NewCodexWebsocketsExecutor(&config.Config{Codex: config.CodexConfig{ResponseSteering: true}})
	exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}
	auth := &coreauth.Auth{ID: "steer-blocking-test", Provider: "codex", Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL, "websockets": "true"}}
	req := core.Request{
		Model:   "gpt-6-astra",
		Payload: []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"start"}]}`),
	}
	opts := core.Options{SourceFormat: translatorreg.FromString("codex"), Metadata: map[string]any{core.ExecutionSessionMetadataKey: "steer-block-test"}}

	result, err := exec.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		t.Fatal(err)
	}

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		eventType := ojson.GetBytes(chunk.Payload, "type").String()
		eventID := ojson.GetBytes(chunk.Payload, "response.id").String()
		if eventType == "response.created" && eventID == "r1" {
			input <- core.WebsocketInput{Payload: []byte(`{"type":"response.steer","previous_response_id":"r1","input":"steer 1"}`)}
		}
		if eventType == "response.created" && eventID == "auto-1" {
			// During automatic successor generation, send a create first, followed by steer:
			input <- core.WebsocketInput{Payload: []byte(`{"type":"response.create","model":"gpt-6-astra","previous_response_id":"auto-1","input":[{"role":"user","content":"queued next"}]}`)}
			input <- core.WebsocketInput{Payload: []byte(`{"type":"response.steer","previous_response_id":"auto-1","input":"steer 2 in flight"}`)}
		}
		if eventType == "response.completed" && eventID == "r2" {
			cancel()
			break
		}
	}

	select {
	case <-serverDone:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not finish")
	}
}
