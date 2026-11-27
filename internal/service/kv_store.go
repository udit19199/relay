package service

import (
	log "github.com/sirupsen/logrus"
	"github.com/udit19199/relay/internal/config"
	"github.com/udit19199/relay/internal/kvstore"
)

func (s *Service) applyKVStoreConfig(cfg *config.Config) {
	if s == nil {
		if errShutdown := kvstore.ShutdownLocal(); errShutdown != nil {
			log.Errorf("failed to shutdown local kv store: %v", errShutdown)
		}
		return
	}
	s.kvStoreManaged.Store(true)
	if cfg == nil || cfg.KVStore.Disabled {
		if errShutdown := kvstore.ShutdownLocal(); errShutdown != nil {
			log.Errorf("failed to shutdown local kv store: %v", errShutdown)
		}
		return
	}
	_ = kvstore.ConfigureLocal(true, cfg.EffectiveKVStorePath())
}

func (s *Service) shutdownKVStore() error {
	if s == nil {
		return nil
	}
	s.kvStoreManaged.Store(false)
	return kvstore.ShutdownLocal()
}
