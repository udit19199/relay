package auth

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

func TestManagerTerminalUnauthorizedDoesNotRefresh(t *testing.T) {
	for _, mode := range []string{"execute", "count", "stream", "bootstrap"} {
		t.Run(mode, func(t *testing.T) {
			terminal := &coreexecutor.RequestTerminatedError{HTTPStatus: http.StatusUnauthorized, Body: []byte(`{"detail":"terminal rejection"}`)}
			executor := &claudeCancellationTestExecutor{
				executeFn: func(context.Context, *Auth) (coreexecutor.Response, error) {
					return coreexecutor.Response{}, terminal
				},
				countFn: func(context.Context, *Auth) (coreexecutor.Response, error) {
					return coreexecutor.Response{}, terminal
				},
				streamFn: func(context.Context, *Auth) (*coreexecutor.StreamResult, error) {
					if mode == "bootstrap" {
						chunks := make(chan coreexecutor.StreamChunk, 1)
						chunks <- coreexecutor.StreamChunk{Err: terminal}
						close(chunks)
						return &coreexecutor.StreamResult{Chunks: chunks}, nil
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
	req := coreexecutor.Request{Model: model}
	switch mode {
	case "execute":
		_, errExecute := manager.Execute(context.Background(), []string{provider}, req, coreexecutor.Options{})
		return errExecute
	case "count":
		_, errCount := manager.ExecuteCount(context.Background(), []string{provider}, req, coreexecutor.Options{})
		return errCount
	default:
		stream, errStream := manager.ExecuteStream(context.Background(), []string{provider}, req, coreexecutor.Options{Stream: true})
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

func requireTerminalResponse(t *testing.T, err error, terminal *coreexecutor.RequestTerminatedError) {
	t.Helper()
	var response *coreexecutor.RequestTerminatedError
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
