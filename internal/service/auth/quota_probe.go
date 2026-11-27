package auth

import (
	"sync/atomic"
	"time"
)

// defaultCredentialQuotaProbeInterval is how often a Claude credential held by
// a credential-wide quota cooldown may carry one real request. The request is
// the only way Relay learns that Anthropic reset the quota before the
// advertised deadline, because Claude has no active quota provider.
const defaultCredentialQuotaProbeInterval = 12 * time.Minute

// quotaProbeClaimRetries bounds how many extra picks a request performs when a
// concurrent request claims the same probe first.
const quotaProbeClaimRetries = 2

// credentialQuotaProbeIntervalSeconds stores the configured probe interval.
// Zero keeps the built-in default; negative values disable probing.
var credentialQuotaProbeIntervalSeconds atomic.Int64

// SetCredentialQuotaProbeIntervalSeconds configures how often an exhausted
// Claude credential may carry one probe request while it stays blocked.
// Zero keeps the built-in 12 minute interval; negative values disable probing.
func SetCredentialQuotaProbeIntervalSeconds(seconds int) {
	credentialQuotaProbeIntervalSeconds.Store(int64(seconds))
}

func credentialQuotaProbeInterval() time.Duration {
	seconds := credentialQuotaProbeIntervalSeconds.Load()
	if seconds < 0 {
		return 0
	}
	if seconds == 0 {
		return defaultCredentialQuotaProbeInterval
	}
	return time.Duration(seconds) * time.Second
}

// credentialQuotaProbeLease bounds how long one admitted probe blocks the next
// one when its result never arrives. Probing must recover on its own; a lost
// stream cannot leave the credential unprobeable for the whole cooldown.
func credentialQuotaProbeLease(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	return 2 * interval
}

// credentialQuotaProbeDue reports whether auth may carry one probe request at
// now. Only Claude credentials are probed: Codex and Devin already refresh
// their own quota state.
func credentialQuotaProbeDue(auth *Auth, now time.Time) bool {
	if !authProviderIs(auth, "claude") {
		return false
	}
	quota := auth.Quota
	if !quota.Exceeded || quota.Reason != "credential_quota" || !quota.NextRecoverAt.After(now) {
		return false
	}
	if credentialQuotaProbeInterval() <= 0 {
		return false
	}
	if quota.ProbeAfter.IsZero() || now.Before(quota.ProbeAfter) {
		return false
	}
	return !quota.ProbeLeaseUntil.After(now)
}

// stampCredentialQuotaProbe restarts the probe clock for a credential that is
// still under a credential-wide quota block. It is called whenever that block
// is recorded or restored, so the first probe waits one full interval.
func stampCredentialQuotaProbe(auth *Auth, now time.Time) {
	if !authProviderIs(auth, "claude") {
		return
	}
	if !auth.Quota.Exceeded || auth.Quota.Reason != "credential_quota" {
		return
	}
	interval := credentialQuotaProbeInterval()
	if interval <= 0 {
		auth.Quota.ProbeAfter = time.Time{}
		auth.Quota.ProbeLeaseUntil = time.Time{}
		return
	}
	auth.Quota.ProbeAfter = now.Add(interval)
	auth.Quota.ProbeLeaseUntil = time.Time{}
}

// claimCredentialQuotaProbe admits the selected auth as the single probe for
// its credential and returns retry=true when another request claimed it first.
// Claiming updates the canonical auth under the manager lock and withdraws the
// credential from the scheduler so concurrent picks stop offering it.
func (m *Manager) claimCredentialQuotaProbe(selected *Auth) bool {
	if m == nil || selected == nil {
		return false
	}
	now := time.Now()
	if !credentialQuotaProbeDue(selected, now) {
		return false
	}
	m.mu.Lock()
	current := m.auths[selected.ID]
	if current == nil {
		m.mu.Unlock()
		return false
	}
	if !credentialQuotaProbeDue(current, now) {
		m.mu.Unlock()
		return true
	}
	interval := credentialQuotaProbeInterval()
	if interval <= 0 {
		m.mu.Unlock()
		return false
	}
	current.Quota.ProbeAfter = now.Add(interval)
	current.Quota.ProbeLeaseUntil = now.Add(credentialQuotaProbeLease(interval))
	snapshot := current.Clone()
	m.mu.Unlock()
	if m.scheduler != nil {
		m.scheduler.upsertAuthResult(snapshot, nil, true)
	}
	return false
}
