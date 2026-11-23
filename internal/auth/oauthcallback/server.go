// Package oauthcallback provides the local loopback HTTP server that receives
// OAuth authorization callbacks for browser-based login flows.
package oauthcallback

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Config holds the provider-specific values of a callback server.
type Config struct {
	CallbackPath       string
	DefaultPlatformURL string
	SuccessHTML        string
	SetupNoticeHTML    string
}

// Server captures the authorization code from an OAuth provider's redirect.
type Server struct {
	cfg        Config
	server     *http.Server
	port       int
	resultChan chan *Result
	errorChan  chan error
	mu         sync.Mutex
	running    bool
}

// Result is the outcome of an OAuth callback: a code and state, or an error.
type Result struct {
	Code  string
	State string
	// ClientID is the dynamically issued client ID, when the provider returns one.
	ClientID string
	Error    string
}

// New creates a callback server for the given port.
func New(port int, cfg Config) *Server {
	return &Server{
		cfg:        cfg,
		port:       port,
		resultChan: make(chan *Result, 1),
		errorChan:  make(chan error, 1),
	}
}

// Start begins listening for the callback and success page requests.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running")
	}
	if !s.isPortAvailable() {
		return fmt.Errorf("port %d is already in use", s.port)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.CallbackPath, s.handleCallback)
	mux.HandleFunc("/success", s.handleSuccess)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	s.running = true

	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.errorChan <- fmt.Errorf("server failed to start: %w", err)
		}
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	return nil
}

// Stop gracefully shuts the server down.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	log.Debug("Stopping OAuth callback server")

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := s.server.Shutdown(shutdownCtx)
	s.running = false
	s.server = nil

	return err
}

// WaitForCallback blocks until a callback result, a server error, or the timeout.
func (s *Server) WaitForCallback(timeout time.Duration) (*Result, error) {
	select {
	case result := <-s.resultChan:
		return result, nil
	case err := <-s.errorChan:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for OAuth callback")
	}
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	log.Debug("Received OAuth callback")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	clientID := strings.TrimSpace(query.Get("client_id"))
	errorParam := query.Get("error")

	if errorParam != "" {
		log.Errorf("OAuth error received: %s", errorParam)
		s.sendResult(&Result{Error: errorParam})
		http.Error(w, fmt.Sprintf("OAuth error: %s", errorParam), http.StatusBadRequest)
		return
	}

	if code == "" {
		log.Error("No authorization code received")
		s.sendResult(&Result{Error: "no_code"})
		http.Error(w, "No authorization code received", http.StatusBadRequest)
		return
	}

	if state == "" {
		log.Error("No state parameter received")
		s.sendResult(&Result{Error: "no_state"})
		http.Error(w, "No state parameter received", http.StatusBadRequest)
		return
	}

	s.sendResult(&Result{Code: code, State: state, ClientID: clientID})
	http.Redirect(w, r, "/success", http.StatusFound)
}

func (s *Server) handleSuccess(w http.ResponseWriter, r *http.Request) {
	log.Debug("Serving success page")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	query := r.URL.Query()
	setupRequired := query.Get("setup_required") == "true"
	platformURL := query.Get("platform_url")
	if platformURL == "" {
		platformURL = s.cfg.DefaultPlatformURL
	}

	if _, err := w.Write([]byte(s.generateSuccessHTML(setupRequired, platformURL))); err != nil {
		log.Errorf("Failed to write success page: %v", err)
	}
}

func (s *Server) generateSuccessHTML(setupRequired bool, platformURL string) string {
	html := strings.ReplaceAll(s.cfg.SuccessHTML, "{{PLATFORM_URL}}", platformURL)
	setupNotice := ""
	if setupRequired {
		setupNotice = strings.ReplaceAll(s.cfg.SetupNoticeHTML, "{{PLATFORM_URL}}", platformURL)
	}
	return strings.Replace(html, "{{SETUP_NOTICE}}", setupNotice, 1)
}

func (s *Server) sendResult(result *Result) {
	select {
	case s.resultChan <- result:
		log.Debug("OAuth result sent to channel")
	default:
		log.Warn("OAuth result channel is full, result dropped")
	}
}

func (s *Server) isPortAvailable() bool {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return false
	}
	defer func() {
		_ = listener.Close()
	}()
	return true
}

// IsRunning reports whether the server is running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
