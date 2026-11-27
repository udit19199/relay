package helps

import (
	"github.com/udit19199/relay/internal/config"
	coreauth "github.com/udit19199/relay/internal/service/auth"
)

// Pooling is configured per api-key credential, so tests opt in through the auth
// override instead of a global config block.
func poolEnabled() *coreauth.ProxyConnectionPoolOverride {
	enabled := true
	return &coreauth.ProxyConnectionPoolOverride{Enabled: &enabled}
}

func poolOptInAuth(id string) *coreauth.Auth {
	return &coreauth.Auth{ID: id, ProxyConnectionPool: poolEnabled()}
}

func poolAuth(id string, override *coreauth.ProxyConnectionPoolOverride) *coreauth.Auth {
	return &coreauth.Auth{ID: id, ProxyConnectionPool: override}
}

// poolBlock is the block with no fields: the opt-in that uses the defaults.
func poolBlock() *coreauth.ProxyConnectionPoolOverride {
	return &coreauth.ProxyConnectionPoolOverride{}
}

func testConfig() *config.Config { return &config.Config{} }
