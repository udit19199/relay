package cliproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// OpenCode Go auth files are served by the OpenAI-compatible executor bound to
// provider "opencode-go" (the synthesizer sets base_url, api_key, and the
// "opencode-go" prefix). Its model catalog is discovered from GET <base_url>/models
// on load and hourly; a failed fetch keeps the last list.
const (
	openCodeGoProvider      = "opencode-go"
	openCodeGoModelsRefresh = time.Hour
	// Model discovery is a short metadata call, bounded like the quota fetch.
	openCodeGoModelsTimeout = 20 * time.Second
)

type openCodeGoCatalog struct {
	apiKey   string
	modelIDs []string
	fetching bool
}

var (
	openCodeGoCatalogsMu sync.Mutex
	openCodeGoCatalogs   = make(map[string]*openCodeGoCatalog)
)

func isOpenCodeGoAuth(a *coreauth.Auth) bool {
	return a != nil && strings.EqualFold(strings.TrimSpace(a.Provider), openCodeGoProvider)
}

// openCodeGoModels returns the last discovered catalog for the auth and starts a
// background fetch when none was fetched yet for its current key.
func (s *Service) openCodeGoModels(a *coreauth.Auth) []*ModelInfo {
	apiKey := strings.TrimSpace(a.Attributes["api_key"])
	openCodeGoCatalogsMu.Lock()
	catalog := openCodeGoCatalogs[a.ID]
	if catalog == nil {
		catalog = &openCodeGoCatalog{}
		openCodeGoCatalogs[a.ID] = catalog
	}
	ids := slices.Clone(catalog.modelIDs)
	startFetch := catalog.apiKey != apiKey && apiKey != "" && !catalog.fetching
	if startFetch {
		catalog.fetching = true
	}
	openCodeGoCatalogsMu.Unlock()
	if startFetch {
		go s.refreshOpenCodeGoModels(context.Background(), a.ID, true)
	}

	now := time.Now().Unix()
	models := make([]*ModelInfo, 0, len(ids))
	for _, id := range ids {
		models = append(models, &ModelInfo{
			ID:          id,
			Object:      "model",
			Created:     now,
			OwnedBy:     openCodeGoProvider,
			Type:        "openai-compatibility",
			DisplayName: id,
			Thinking:    &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
		})
	}
	return models
}

// refreshOpenCodeGoModels fetches the catalog for one auth and re-registers its
// models when the list changed. claimed reports whether the caller already set
// the fetching flag.
func (s *Service) refreshOpenCodeGoModels(ctx context.Context, authID string, claimed bool) {
	if s == nil || s.coreManager == nil {
		return
	}
	openCodeGoCatalogsMu.Lock()
	catalog := openCodeGoCatalogs[authID]
	if catalog == nil {
		catalog = &openCodeGoCatalog{}
		openCodeGoCatalogs[authID] = catalog
	}
	if !claimed {
		if catalog.fetching {
			openCodeGoCatalogsMu.Unlock()
			return
		}
		catalog.fetching = true
	}
	openCodeGoCatalogsMu.Unlock()

	auth, ok := s.coreManager.GetByID(authID)
	if !ok || !isOpenCodeGoAuth(auth) || auth.Disabled {
		openCodeGoCatalogsMu.Lock()
		delete(openCodeGoCatalogs, authID)
		openCodeGoCatalogsMu.Unlock()
		return
	}
	apiKey := strings.TrimSpace(auth.Attributes["api_key"])
	ids, errFetch := s.fetchOpenCodeGoModelIDs(ctx, auth, apiKey)

	openCodeGoCatalogsMu.Lock()
	catalog.fetching = false
	changed := false
	if errFetch == nil {
		catalog.apiKey = apiKey
		changed = !slices.Equal(catalog.modelIDs, ids)
		catalog.modelIDs = ids
	}
	openCodeGoCatalogsMu.Unlock()

	if errFetch != nil {
		log.WithError(errFetch).Warnf("opencode-go: model list refresh failed for %s; keeping the last list", authID)
		return
	}
	if changed {
		log.Infof("opencode-go: discovered %d models for %s", len(ids), authID)
		s.refreshModelRegistrationForAuth(auth)
	}
}

func (s *Service) fetchOpenCodeGoModelIDs(ctx context.Context, auth *coreauth.Auth, apiKey string) ([]string, error) {
	baseURL := strings.TrimSpace(auth.Attributes["base_url"])
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("missing base_url or api_key")
	}
	ctx, cancel := context.WithTimeout(ctx, openCodeGoModelsTimeout)
	defer cancel()
	req, errReq := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/models", nil)
	if errReq != nil {
		return nil, fmt.Errorf("build models request: %w", errReq)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "relay")
	resp, errDo := helps.NewProxyAwareHTTPClient(ctx, s.currentConfig(), auth, 0).Do(req)
	if errDo != nil {
		return nil, fmt.Errorf("models request failed: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Debugf("opencode-go: close models response: %v", errClose)
		}
	}()
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if errRead != nil {
		return nil, fmt.Errorf("read models response: %w", errRead)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models request returned status %d", resp.StatusCode)
	}
	var decoded struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if errJSON := json.Unmarshal(body, &decoded); errJSON != nil {
		return nil, fmt.Errorf("models response is not valid JSON")
	}
	ids := make([]string, 0, len(decoded.Data))
	for _, model := range decoded.Data {
		if id := strings.TrimSpace(model.ID); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("models response lists no models")
	}
	slices.Sort(ids)
	return ids, nil
}

// startOpenCodeGoModelRefresh re-fetches every OpenCode Go catalog hourly.
func (s *Service) startOpenCodeGoModelRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(openCodeGoModelsRefresh)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if s.coreManager == nil {
				continue
			}
			for _, auth := range s.coreManager.List() {
				if isOpenCodeGoAuth(auth) && !auth.Disabled {
					s.refreshOpenCodeGoModels(ctx, auth.ID, false)
				}
			}
		}
	}()
}
