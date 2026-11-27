package helps

import (
	"strings"

	"github.com/google/uuid"
	"github.com/udit19199/relay/internal/misc"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
	coresession "github.com/udit19199/relay/internal/service/session"
)

// DerivedSessionID returns the first context-derived session identity in metadata order.
func DerivedSessionID(metadataSets ...map[string]any) string {
	for _, metadata := range metadataSets {
		if derivedID := coresession.DerivedID(metadata); derivedID != "" {
			return derivedID
		}
	}
	return ""
}

// DerivedSessionUUID maps a derived session identity to a provider-scoped stable UUID.
func DerivedSessionUUID(provider string, metadataSets ...map[string]any) string {
	return stableProviderSessionUUID(provider, "derived-session", DerivedSessionID(metadataSets...))
}

// ProviderSessionUUID prefers a long-lived execution session and falls back to the derived identity.
func ProviderSessionUUID(provider string, metadataSets ...map[string]any) string {
	for _, metadata := range metadataSets {
		if executionID := misc.MetadataString(metadata, coreexecutor.ExecutionSessionMetadataKey); executionID != "" {
			return stableProviderSessionUUID(provider, "execution-session", executionID)
		}
	}
	return DerivedSessionUUID(provider, metadataSets...)
}

func stableProviderSessionUUID(provider string, kind string, identityValue string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	identityValue = strings.TrimSpace(identityValue)
	if provider == "" || identityValue == "" {
		return ""
	}
	identity := strings.Join([]string{"cli-proxy-api", provider, kind, identityValue}, "\x00")
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(identity)).String()
}
