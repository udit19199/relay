package management

import "encoding/json"

// QuotaSubscription describes plan and tier information in normalized quota.
type QuotaSubscription struct {
	Plan     string `json:"plan,omitempty"`
	TierName string `json:"tierName,omitempty"`
	TierID   string `json:"tierId,omitempty"`
}

// UnmarshalJSON supports both camelCase and snake_case field names.
func (s *QuotaSubscription) UnmarshalJSON(data []byte) error {
	type Alias QuotaSubscription
	aux := struct {
		*Alias
		AltTierName string `json:"tier_name"`
		AltTierID   string `json:"tier_id"`
	}{Alias: (*Alias)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if s.TierName == "" && aux.AltTierName != "" {
		s.TierName = aux.AltTierName
	}
	if s.TierID == "" && aux.AltTierID != "" {
		s.TierID = aux.AltTierID
	}
	return nil
}

// QuotaGroup describes a group of quota buckets in normalized quota.
type QuotaGroup struct {
	DisplayName string        `json:"displayName,omitempty"`
	Buckets     []QuotaBucket `json:"buckets,omitempty"`
}

// UnmarshalJSON supports both camelCase and snake_case field names.
func (g *QuotaGroup) UnmarshalJSON(data []byte) error {
	type Alias QuotaGroup
	aux := struct {
		*Alias
		AltDisplayName string `json:"display_name"`
	}{Alias: (*Alias)(g)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if g.DisplayName == "" && aux.AltDisplayName != "" {
		g.DisplayName = aux.AltDisplayName
	}
	return nil
}

// QuotaBucket describes a single quota window or limit bucket.
type QuotaBucket struct {
	Window            string  `json:"window,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
	Description       string  `json:"description,omitempty"`
}

// UnmarshalJSON supports both camelCase and snake_case field names.
func (b *QuotaBucket) UnmarshalJSON(data []byte) error {
	type Alias QuotaBucket
	aux := struct {
		*Alias
		RemainingFraction    *float64 `json:"remainingFraction"`
		AltRemainingFraction *float64 `json:"remaining_fraction"`
		AltResetTime         string   `json:"reset_time"`
	}{Alias: (*Alias)(b)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.RemainingFraction != nil {
		b.RemainingFraction = *aux.RemainingFraction
	} else if aux.AltRemainingFraction != nil {
		b.RemainingFraction = *aux.AltRemainingFraction
	}
	if b.ResetTime == "" && aux.AltResetTime != "" {
		b.ResetTime = aux.AltResetTime
	}
	return nil
}

// QuotaMetric is a bounded numeric account summary for management UI rendering.
type QuotaMetric struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	Format   string  `json:"format,omitempty"`
	Currency string  `json:"currency,omitempty"`
}

// QuotaFetchResponse carries normalized quota information for management UI rendering.
type QuotaFetchResponse struct {
	Subscription       *QuotaSubscription `json:"subscription,omitempty"`
	Summary            []QuotaMetric      `json:"summary,omitempty"`
	ServerTimeOffsetMs int64              `json:"serverTimeOffsetMs,omitempty"`
	Groups             []QuotaGroup       `json:"groups,omitempty"`
}

// UnmarshalJSON supports both camelCase and snake_case field names.
func (r *QuotaFetchResponse) UnmarshalJSON(data []byte) error {
	type Alias QuotaFetchResponse
	aux := struct {
		*Alias
		AltServerTimeOffsetMs int64 `json:"server_time_offset_ms"`
	}{Alias: (*Alias)(r)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if r.ServerTimeOffsetMs == 0 && aux.AltServerTimeOffsetMs != 0 {
		r.ServerTimeOffsetMs = aux.AltServerTimeOffsetMs
	}
	return nil
}
