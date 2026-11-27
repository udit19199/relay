package service

import (
	"context"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/udit19199/relay/internal/registry"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

const (
	modelRegistrationMaxWorkersPerCategory         = 5
	modelRegistrationMaxWorkersOpenAICompatibility = 20
)

const (
	modelRegistrationPhaseConfigAPIKey = iota
	modelRegistrationPhaseOther
)

type modelRegistrationTask struct {
	phase    int
	category string
	run      func(*openAICompatibilityRegistrationCache)
	done     func()
}

type executorRegistrationOptions struct {
	includeBaseline   bool
	forceReplaceAuths bool
	auths             []*coreauth.Auth
}

// modelRegistrationTaskHook, if set, runs after auth-update commits and before
// model registration workers start. Tests use it to prove registration no longer
// holds authUpdateMu.
var modelRegistrationTaskHook func()

// modelRegistrationTaskPostRunHook, if set, runs synchronously inside model registration
// tasks after completeModelRegistrationForAuthWithCache completes. Tests use it to inject
// concurrent auth updates before batch registration finishes.
var modelRegistrationTaskPostRunHook func(authID string)

func (s *Service) registerModelsForAuthBatch(ctx context.Context, auths []*coreauth.Auth) {
	if s == nil || s.coreManager == nil || len(auths) == 0 {
		return
	}
	var registeredGen sync.Map
	tasks := make([]modelRegistrationTask, 0, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		authForRegistration := auth.Clone()
		tasks = append(tasks, modelRegistrationTask{
			phase:    modelRegistrationPhase(authForRegistration),
			category: modelRegistrationCategory(authForRegistration),
			run: func(compatCache *openAICompatibilityRegistrationCache) {
				gen := s.completeModelRegistrationForAuthWithCache(ctx, authForRegistration, compatCache)
				registeredGen.Store(authForRegistration.ID, gen)
				if modelRegistrationTaskPostRunHook != nil {
					modelRegistrationTaskPostRunHook(authForRegistration.ID)
				}
			},
		})
	}
	s.runModelRegistrationTasks(ctx, tasks)
	if ctx != nil && ctx.Err() != nil {
		return
	}

	compatCache := s.newOpenAICompatibilityRegistrationCache()
	for _, latest := range s.coreManager.List() {
		if latest == nil || latest.ID == "" {
			continue
		}
		lastGenVal, loaded := registeredGen.Load(latest.ID)
		lastGen, _ := lastGenVal.(uint64)
		if !loaded || latest.Generation != lastGen {
			s.ensureExecutorsForAuthWithContext(ctx, latest, false)
			s.completeModelRegistrationForAuthWithCache(ctx, latest, compatCache)
		}
	}
}

func (s *Service) runModelRegistrationTasks(ctx context.Context, tasks []modelRegistrationTask) {
	if len(tasks) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	configAPIKeyTasks := make([]modelRegistrationTask, 0)
	otherTasks := make([]modelRegistrationTask, 0)
	for _, task := range tasks {
		if task.phase == modelRegistrationPhaseConfigAPIKey {
			configAPIKeyTasks = append(configAPIKeyTasks, task)
			continue
		}
		otherTasks = append(otherTasks, task)
	}

	compatCache := s.newOpenAICompatibilityRegistrationCache()
	s.runModelRegistrationTaskPhase(ctx, configAPIKeyTasks, compatCache)
	s.runModelRegistrationTaskPhase(ctx, otherTasks, compatCache)
}

func (s *Service) runModelRegistrationTaskPhase(ctx context.Context, tasks []modelRegistrationTask, compatCache *openAICompatibilityRegistrationCache) {
	if len(tasks) == 0 {
		return
	}

	grouped := make(map[string][]modelRegistrationTask)
	order := make([]string, 0)
	for _, task := range tasks {
		if task.run == nil {
			continue
		}
		category := strings.ToLower(strings.TrimSpace(task.category))
		if category == "" {
			category = "unknown"
		}
		if _, exists := grouped[category]; !exists {
			order = append(order, category)
		}
		grouped[category] = append(grouped[category], task)
	}

	var wg sync.WaitGroup
	for _, category := range order {
		group := grouped[category]
		workers := len(group)
		maxWorkers := modelRegistrationMaxWorkersForCategory(category)
		if workers > maxWorkers {
			workers = maxWorkers
		}
		if workers <= 0 {
			continue
		}

		taskCh := make(chan modelRegistrationTask)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for task := range taskCh {
					func(task modelRegistrationTask) {
						if task.done != nil {
							defer task.done()
						}
						select {
						case <-ctx.Done():
							return
						default:
						}
						if modelRegistrationTaskHook != nil {
							modelRegistrationTaskHook()
						}
						task.run(compatCache)
					}(task)
				}
			}()
		}
		go func(group []modelRegistrationTask) {
			defer close(taskCh)
			for _, task := range group {
				select {
				case <-ctx.Done():
					return
				case taskCh <- task:
				}
			}
		}(group)
	}
	wg.Wait()
}

func modelRegistrationPhase(auth *coreauth.Auth) int {
	if coreauth.IsConfigAPIKeyAuth(auth) {
		return modelRegistrationPhaseConfigAPIKey
	}
	return modelRegistrationPhaseOther
}

func modelRegistrationCategory(auth *coreauth.Auth) string {
	if auth == nil {
		return "unknown"
	}
	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	if compatProviderKey, _, compatDetected := openAICompatInfoFromAuth(auth); compatDetected {
		if compatProviderKey != "" {
			provider = compatProviderKey
		} else {
			provider = "openai-compatibility"
		}
	}
	if provider == "" {
		provider = "unknown"
	}

	authKind := auth.AuthKind()
	if authKind == "" {
		return provider
	}
	return provider + ":" + authKind
}

func modelRegistrationMaxWorkersForCategory(category string) int {
	category = strings.ToLower(strings.TrimSpace(category))
	if strings.HasPrefix(category, "openai-compatible-") || strings.HasPrefix(category, "openai-compatibility") {
		return modelRegistrationMaxWorkersOpenAICompatibility
	}
	return modelRegistrationMaxWorkersPerCategory
}

func (s *Service) registerModelRefreshCallback() {
	// Register callback for startup and periodic model catalog refresh.
	// When remote model definitions change, re-register models for affected providers.
	// This intentionally rebuilds per-auth model availability from the latest catalog
	// snapshot instead of preserving prior registry suppression state.
	registry.SetModelRefreshCallback(func(changedProviders []string) {
		if s == nil || s.coreManager == nil || len(changedProviders) == 0 {
			return
		}

		providerSet := make(map[string]bool, len(changedProviders))
		for _, p := range changedProviders {
			norm := strings.ToLower(strings.TrimSpace(p))
			if norm != "" {
				providerSet[norm] = true
			}
		}

		auths := s.coreManager.List()
		refreshed := 0
		var refreshedMu sync.Mutex
		tasks := make([]modelRegistrationTask, 0, len(auths))
		for _, item := range auths {
			if item == nil || item.ID == "" {
				continue
			}
			auth, ok := s.coreManager.GetByID(item.ID)
			if !ok || auth == nil || auth.Disabled {
				continue
			}
			provider := strings.ToLower(strings.TrimSpace(auth.Provider))
			if !providerSet[provider] {
				continue
			}
			authForRefresh := auth
			tasks = append(tasks, modelRegistrationTask{
				phase:    modelRegistrationPhase(authForRefresh),
				category: modelRegistrationCategory(authForRefresh),
				run: func(compatCache *openAICompatibilityRegistrationCache) {
					if s.refreshModelRegistrationForAuthWithCache(authForRefresh, compatCache) {
						refreshedMu.Lock()
						refreshed++
						refreshedMu.Unlock()
					}
				},
			})
		}
		s.runModelRegistrationTasks(context.Background(), tasks)

		if refreshed > 0 {
			log.Infof("re-registered models for %d auth(s) due to model catalog changes: %v", refreshed, changedProviders)
		}
	})
}
