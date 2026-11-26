package auth

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type credentialTerminalResponseError struct{ error }

func (e credentialTerminalResponseError) Unwrap() error          { return e.error }
func (credentialTerminalResponseError) IsCredentialScoped() bool { return true }

func TestManagerTerminalUnauthorizedDoesNotRefresh(t *testing.T) {
	for _, mode := range []string{"execute", "count", "stream", "bootstrap"} {
		t.Run(mode, func(t *testing.T) {
			terminal := &cliproxyexecutor.RequestTerminatedError{HTTPStatus: http.StatusUnauthorized, Body: []byte(`{"detail":"terminal rejection"}`)}
			executor := &claudeCancellationTestExecutor{
				executeFn: func(context.Context, *Auth) (cliproxyexecutor.Response, error) {
					return cliproxyexecutor.Response{}, terminal
				},
				countFn: func(context.Context, *Auth) (cliproxyexecutor.Response, error) {
					return cliproxyexecutor.Response{}, terminal
				},
				streamFn: func(context.Context, *Auth) (*cliproxyexecutor.StreamResult, error) {
					if mode == "bootstrap" {
						chunks := make(chan cliproxyexecutor.StreamChunk, 1)
						chunks <- cliproxyexecutor.StreamChunk{Err: terminal}
						close(chunks)
						return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
					}
					return nil, terminal
				},
			}
			manager, auth, model := newClaudeCancellationTestManager(t, executor, nil)
			requireTerminalResponse(t, invokeTerminalResponse(manager, executor.Identifier(), model, mode), terminal)
			if calls := executor.executeCalls.Load() + executor.countCalls.Load() + executor.streamCalls.Load(); calls != 1 || executor.refreshCalls.Load() != 0 {
				t.Fatalf("terminal 401 was retried or refreshed: executions=%d refreshes=%d", calls, executor.refreshCalls.Load())
			}
			requireClaudeCancellationNeutral(t, manager, auth.ID, model)
		})
	}
}

func invokeTerminalResponse(manager *Manager, provider, model, mode string) error {
	req := cliproxyexecutor.Request{Model: model}
	switch mode {
	case "execute":
		_, errExecute := manager.Execute(context.Background(), []string{provider}, req, cliproxyexecutor.Options{})
		return errExecute
	case "count":
		_, errCount := manager.ExecuteCount(context.Background(), []string{provider}, req, cliproxyexecutor.Options{})
		return errCount
	default:
		stream, errStream := manager.ExecuteStream(context.Background(), []string{provider}, req, cliproxyexecutor.Options{Stream: true})
		if errStream != nil {
			return errStream
		}
		for chunk := range stream.Chunks {
			if chunk.Err != nil {
				return chunk.Err
			}
		}
		return nil
	}
}

func requireTerminalResponse(t *testing.T, err error, terminal *cliproxyexecutor.RequestTerminatedError) {
	t.Helper()
	var response *cliproxyexecutor.RequestTerminatedError
	if !errors.As(err, &response) || response != terminal {
		t.Fatalf("response = %v, want original terminal response", err)
	}
	var downstream interface {
		StatusCode() int
		ResponseHeaders() http.Header
		ResponseBody() []byte
	}
	if !errors.As(err, &downstream) || downstream.StatusCode() != terminal.HTTPStatus ||
		!reflect.DeepEqual(downstream.ResponseHeaders(), terminal.Header) || string(downstream.ResponseBody()) != string(terminal.Body) {
		t.Fatalf("downstream status, headers or body changed: %v", err)
	}
}
