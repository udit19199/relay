package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	xcurrency "golang.org/x/text/currency"
)

type credentialQuotaRequest struct {
	AuthIndexSnake  *string `json:"auth_index"`
	AuthIndexCamel  *string `json:"authIndex"`
	AuthIndexPascal *string `json:"AuthIndex"`
	Provider        string  `json:"provider"`
}

func (r credentialQuotaRequest) resolveAuthIndex() string {
	if r.AuthIndexSnake != nil && strings.TrimSpace(*r.AuthIndexSnake) != "" {
		return strings.TrimSpace(*r.AuthIndexSnake)
	}
	if r.AuthIndexCamel != nil && strings.TrimSpace(*r.AuthIndexCamel) != "" {
		return strings.TrimSpace(*r.AuthIndexCamel)
	}
	if r.AuthIndexPascal != nil && strings.TrimSpace(*r.AuthIndexPascal) != "" {
		return strings.TrimSpace(*r.AuthIndexPascal)
	}
	return ""
}

// FetchCredentialQuota retrieves normalized quota for a credential via its quota provider or declarative probe.
func (h *Handler) FetchCredentialQuota(c *gin.Context) {
	var body credentialQuotaRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	authIndex := body.resolveAuthIndex()
	if authIndex == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
		return
	}

	auth := h.authByIndex(authIndex)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
		return
	}

	if isClaudeAuth(auth) {
		quotaResp, errFetch := h.fetchClaudeQuota(c.Request.Context(), auth)
		if errFetch != nil {
			log.WithError(errFetch).Warnf("failed to fetch quota for credential %s", auth.Index)
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to fetch quota: %v", errFetch)})
			return
		}
		c.JSON(http.StatusOK, quotaResp)
		return
	}

	if isOpenCodeGoAuth(auth) {
		quotaResp, errFetch := h.fetchOpenCodeGoQuota(c.Request.Context(), auth)
		if errors.Is(errFetch, errOpenCodeGoKeyRejected) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": errFetch.Error()})
			return
		}
		if errFetch != nil {
			log.WithError(errFetch).Warnf("failed to fetch quota for credential %s", auth.Index)
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to fetch quota: %v", errFetch)})
			return
		}
		c.JSON(http.StatusOK, quotaResp)
		return
	}

	// Fallback to declarative quota probe if configured in metadata
	if auth.Metadata != nil {
		if rawProbe, okProbe := auth.Metadata["quota_probe"]; okProbe && rawProbe != nil {
			if probeMap, okMap := rawProbe.(map[string]any); okMap {
				quotaResp, handledProbe, errProbe := h.executeQuotaProbe(c, auth, probeMap)
				if handledProbe {
					if errProbe != nil {
						c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("quota probe failed: %v", errProbe)})
						return
					}
					c.JSON(http.StatusOK, quotaResp)
					return
				}
			}
		}
	}

	c.JSON(http.StatusNotImplemented, gin.H{"error": "no quota provider available for credential"})
}

func (h *Handler) executeQuotaProbe(c *gin.Context, auth *coreauth.Auth, probe map[string]any) (QuotaFetchResponse, bool, error) {
	urlStr, _ := probe["url"].(string)
	urlStr = strings.TrimSpace(urlStr)
	if urlStr == "" {
		return QuotaFetchResponse{}, false, nil
	}

	method, _ := probe["method"].(string)
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}

	needsToken := strings.Contains(urlStr, "$TOKEN$")
	rawData, _ := probe["data"].(string)
	if strings.Contains(rawData, "$TOKEN$") {
		needsToken = true
	}
	headers, _ := probe["header"].(map[string]any)
	if headers == nil {
		headers, _ = probe["headers"].(map[string]any)
	}
	for _, v := range headers {
		if strVal, okVal := v.(string); okVal && strings.Contains(strVal, "$TOKEN$") {
			needsToken = true
			break
		}
	}

	var token string
	if needsToken {
		var errToken error
		token, errToken = h.resolveTokenForAuth(c.Request.Context(), auth, "")
		if errToken != nil {
			return QuotaFetchResponse{}, true, fmt.Errorf("probe authentication failed: %w", errToken)
		}
		if token == "" {
			return QuotaFetchResponse{}, true, fmt.Errorf("probe authentication token not found for credential")
		}
		urlStr = strings.ReplaceAll(urlStr, "$TOKEN$", token)
		rawData = strings.ReplaceAll(rawData, "$TOKEN$", token)
	}

	var reqBody io.Reader
	if rawData != "" {
		reqBody = strings.NewReader(rawData)
	}

	req, errReq := http.NewRequestWithContext(c.Request.Context(), method, urlStr, reqBody)
	if errReq != nil {
		return QuotaFetchResponse{}, false, fmt.Errorf("build probe request: %w", errReq)
	}

	for k, v := range headers {
		if strVal, okVal := v.(string); okVal {
			if needsToken {
				strVal = strings.ReplaceAll(strVal, "$TOKEN$", token)
			}
			req.Header.Set(k, strVal)
		}
	}

	client := &http.Client{
		Transport: h.apiCallTransport(auth, ""),
	}

	resp, errDo := client.Do(req)
	if errDo != nil {
		return QuotaFetchResponse{}, true, fmt.Errorf("probe request failed: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("probe response body close error: %v", errClose)
		}
	}()

	respBytes, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return QuotaFetchResponse{}, true, fmt.Errorf("read probe response: %w", errRead)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return QuotaFetchResponse{}, true, fmt.Errorf("probe returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	if !json.Valid(respBytes) {
		return QuotaFetchResponse{}, true, fmt.Errorf("upstream probe response is not valid JSON")
	}

	var serverOffsetMs int64
	if dateHeader := resp.Header.Get("Date"); dateHeader != "" {
		if parsedDate, errDate := http.ParseTime(dateHeader); errDate == nil {
			serverOffsetMs = parsedDate.Sub(time.Now()).Milliseconds()
		}
	}

	if mappingRaw, okMapping := probe["mapping"].(map[string]any); okMapping {
		mappedResp, errMap := mapProbeResponse(respBytes, mappingRaw)
		if errMap != nil {
			return QuotaFetchResponse{}, true, fmt.Errorf("probe response mapping failed: %w", errMap)
		}
		if mappedResp.ServerTimeOffsetMs == 0 {
			mappedResp.ServerTimeOffsetMs = serverOffsetMs
		}
		return mappedResp, true, nil
	}

	var rawQuota map[string]json.RawMessage
	if errRaw := json.Unmarshal(respBytes, &rawQuota); errRaw == nil {
		for key := range rawQuota {
			if strings.EqualFold(key, "summary") {
				delete(rawQuota, key) // Optional upstream data must not invalidate core quota fields.
			}
		}
		if coreQuotaJSON, errMarshal := json.Marshal(rawQuota); errMarshal == nil {
			var quotaResp QuotaFetchResponse
			if errJSON := json.Unmarshal(coreQuotaJSON, &quotaResp); errJSON == nil {
				hasPlan := quotaResp.Subscription != nil && strings.TrimSpace(quotaResp.Subscription.Plan) != ""
				// Filter groups to retain only buckets with an actual valid numeric remaining fraction in raw JSON
				filteredGroups := make([]QuotaGroup, 0, len(quotaResp.Groups))
				groupsRes := ojson.GetBytesNoCopy(respBytes, "groups")
				if groupsRes.IsArray() {
					for gIdx, grp := range groupsRes.Array() {
						if gIdx >= len(quotaResp.Groups) {
							break
						}
						bucketsRes := grp.Get("buckets")
						if !bucketsRes.IsArray() {
							continue
						}
						origGroup := quotaResp.Groups[gIdx]
						validBuckets := make([]QuotaBucket, 0, len(origGroup.Buckets))
						for bIdx, bkt := range bucketsRes.Array() {
							if bIdx >= len(origGroup.Buckets) {
								break
							}
							remFrac := bkt.Get("remainingFraction")
							if !remFrac.Exists() {
								remFrac = bkt.Get("remaining_fraction")
							}
							if fracVal, okNum := parseNumericFraction(remFrac); okNum {
								bucket := origGroup.Buckets[bIdx]
								bucket.RemainingFraction = fracVal
								validBuckets = append(validBuckets, bucket)
							}
						}
						if len(validBuckets) > 0 {
							origGroup.Buckets = validBuckets
							filteredGroups = append(filteredGroups, origGroup)
						}
					}
				}
				quotaResp.Groups = filteredGroups
				quotaResp.Summary = filterUsableQuotaSummary(respBytes)
				hasValidBuckets := len(filteredGroups) > 0
				hasValidSummary := len(quotaResp.Summary) > 0
				if hasPlan || hasValidBuckets || hasValidSummary {
					if quotaResp.ServerTimeOffsetMs == 0 {
						quotaResp.ServerTimeOffsetMs = serverOffsetMs
					}
					return quotaResp, true, nil
				}
			}
		}
	}

	return QuotaFetchResponse{}, true, fmt.Errorf("upstream probe response does not match normalized quota shape or declared mapping")
}

func filterUsableQuotaSummary(raw []byte) []QuotaMetric {
	var rawQuota map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawQuota); err != nil {
		return nil
	}
	rawSummary, ok := rawQuota["summary"]
	if !ok {
		for key, value := range rawQuota {
			if strings.EqualFold(key, "summary") {
				rawSummary = value
				break
			}
		}
	}
	summaryResult := util.ParseGJSONBytesNoCopy(rawSummary)
	if !summaryResult.IsArray() {
		return nil
	}
	usable := make([]QuotaMetric, 0, len(summaryResult.Array()))
	for _, rawMetric := range summaryResult.Array() {
		keyResult := rawMetric.Get("key")
		labelResult := rawMetric.Get("label")
		key := strings.TrimSpace(keyResult.String())
		label := strings.TrimSpace(labelResult.String())
		value := rawMetric.Get("value")
		if keyResult.Type != ojson.String || labelResult.Type != ojson.String || key == "" || label == "" || value.Type != ojson.Number || math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) {
			continue
		}
		metric := QuotaMetric{
			Key:   strings.Clone(key),
			Label: strings.Clone(label),
			Value: value.Float(),
		}
		if unitResult := rawMetric.Get("unit"); unitResult.Type == ojson.String {
			metric.Unit = strings.Clone(strings.TrimSpace(unitResult.String()))
		}
		if formatResult := rawMetric.Get("format"); formatResult.Type == ojson.String {
			format := strings.TrimSpace(formatResult.String())
			switch format {
			case "number":
				metric.Format = strings.Clone(format)
			case "currency":
				if currencyResult := rawMetric.Get("currency"); currencyResult.Type == ojson.String {
					code := strings.ToUpper(strings.TrimSpace(currencyResult.String()))
					if _, err := xcurrency.ParseISO(code); err == nil {
						metric.Format = strings.Clone(format)
						metric.Currency = strings.Clone(code)
					}
				}
			}
		}
		usable = append(usable, metric)
	}
	return usable
}

func parseNumericFraction(res ojson.Result) (float64, bool) {
	if !res.Exists() {
		return 0, false
	}
	switch res.Type {
	case ojson.Number:
		val := res.Float()
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return 0, false
		}
		return val, true
	case ojson.String:
		s := strings.TrimSpace(res.String())
		if s == "" {
			return 0, false
		}
		val, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
			return 0, false
		}
		return val, true
	default:
		return 0, false
	}
}

func mapProbeResponse(respBytes []byte, mapping map[string]any) (QuotaFetchResponse, error) {
	out := QuotaFetchResponse{}
	if planPath, ok := mapping["plan"].(string); ok && planPath != "" {
		if res := ojson.GetBytesNoCopy(respBytes, planPath); res.Exists() && strings.TrimSpace(res.String()) != "" {
			if out.Subscription == nil {
				out.Subscription = &QuotaSubscription{}
			}
			out.Subscription.Plan = strings.Clone(res.String())
		}
	}
	if tierPath, ok := mapping["tier_name"].(string); ok && tierPath != "" {
		if res := ojson.GetBytesNoCopy(respBytes, tierPath); res.Exists() && strings.TrimSpace(res.String()) != "" {
			if out.Subscription == nil {
				out.Subscription = &QuotaSubscription{}
			}
			out.Subscription.TierName = strings.Clone(res.String())
		}
	} else if tierPath, ok := mapping["tierName"].(string); ok && tierPath != "" {
		if res := ojson.GetBytesNoCopy(respBytes, tierPath); res.Exists() && strings.TrimSpace(res.String()) != "" {
			if out.Subscription == nil {
				out.Subscription = &QuotaSubscription{}
			}
			out.Subscription.TierName = strings.Clone(res.String())
		}
	}
	if tierIDPath, ok := mapping["tier_id"].(string); ok && tierIDPath != "" {
		if res := ojson.GetBytesNoCopy(respBytes, tierIDPath); res.Exists() && strings.TrimSpace(res.String()) != "" {
			if out.Subscription == nil {
				out.Subscription = &QuotaSubscription{}
			}
			out.Subscription.TierID = strings.Clone(res.String())
		}
	} else if tierIDPath, ok := mapping["tierId"].(string); ok && tierIDPath != "" {
		if res := ojson.GetBytesNoCopy(respBytes, tierIDPath); res.Exists() && strings.TrimSpace(res.String()) != "" {
			if out.Subscription == nil {
				out.Subscription = &QuotaSubscription{}
			}
			out.Subscription.TierID = strings.Clone(res.String())
		}
	}

	rawGroups, okGroups := mapping["groups"].([]any)
	if okGroups {
		for _, rg := range rawGroups {
			gm, okGM := rg.(map[string]any)
			if !okGM {
				continue
			}
			group := QuotaGroup{}
			if namePath, okName := gm["display_name"].(string); okName {
				if res := ojson.GetBytesNoCopy(respBytes, namePath); res.Exists() && strings.TrimSpace(res.String()) != "" {
					group.DisplayName = strings.Clone(res.String())
				} else {
					group.DisplayName = namePath
				}
			} else if namePath, okName := gm["displayName"].(string); okName {
				if res := ojson.GetBytesNoCopy(respBytes, namePath); res.Exists() && strings.TrimSpace(res.String()) != "" {
					group.DisplayName = strings.Clone(res.String())
				} else {
					group.DisplayName = namePath
				}
			}

			if bucketsPath, okBP := gm["buckets_path"].(string); okBP && bucketsPath != "" {
				arrayRes := ojson.GetBytesNoCopy(respBytes, bucketsPath)
				if arrayRes.IsArray() && len(arrayRes.Array()) > 0 {
					winKey, _ := gm["window_key"].(string)
					if winKey == "" {
						winKey = "window"
					}
					remFracKey, _ := gm["remaining_fraction_key"].(string)
					if remFracKey == "" {
						remFracKey = "remaining_fraction"
					}
					remAmtKey, _ := gm["remaining_amount_key"].(string)
					totAmtKey, _ := gm["total_amount_key"].(string)
					resetKey, _ := gm["reset_time_key"].(string)
					if resetKey == "" {
						resetKey = "reset_time"
					}
					descKey, _ := gm["description_key"].(string)
					if descKey == "" {
						descKey = "description"
					}

					for _, item := range arrayRes.Array() {
						var hasFraction bool
						var frac float64
						if remFracKey != "" {
							if val, ok := parseNumericFraction(item.Get(remFracKey)); ok {
								frac = val
								hasFraction = true
							}
						}
						if !hasFraction && remAmtKey != "" && totAmtKey != "" {
							remVal, okRem := parseNumericFraction(item.Get(remAmtKey))
							totVal, okTot := parseNumericFraction(item.Get(totAmtKey))
							if okRem && okTot && totVal > 0 {
								frac = remVal / totVal
								hasFraction = true
							}
						}
						if !hasFraction {
							continue
						}
						bucket := QuotaBucket{
							Window:            strings.Clone(item.Get(winKey).String()),
							RemainingFraction: frac,
							ResetTime:         strings.Clone(item.Get(resetKey).String()),
							Description:       strings.Clone(item.Get(descKey).String()),
						}
						group.Buckets = append(group.Buckets, bucket)
					}
				}
			}

			if rawBuckets, okRB := gm["buckets"].([]any); okRB {
				for _, rb := range rawBuckets {
					bm, okBM := rb.(map[string]any)
					if !okBM {
						continue
					}
					var hasFraction bool
					var frac float64
					if rf, ok := bm["remaining_fraction"].(string); ok && rf != "" {
						if val, okNum := parseNumericFraction(ojson.GetBytesNoCopy(respBytes, rf)); okNum {
							frac = val
							hasFraction = true
						}
					}
					if !hasFraction {
						if rem, okRem := bm["remaining_amount"].(string); okRem && rem != "" {
							if tot, okTot := bm["total_amount"].(string); okTot && tot != "" {
								remVal, okRemNum := parseNumericFraction(ojson.GetBytesNoCopy(respBytes, rem))
								totVal, okTotNum := parseNumericFraction(ojson.GetBytesNoCopy(respBytes, tot))
								if okRemNum && okTotNum && totVal > 0 {
									frac = remVal / totVal
									hasFraction = true
								}
							}
						}
					}
					if !hasFraction {
						continue
					}
					bucket := QuotaBucket{
						RemainingFraction: frac,
					}
					if win, ok := bm["window"].(string); ok {
						if res := ojson.GetBytesNoCopy(respBytes, win); res.Exists() {
							bucket.Window = strings.Clone(res.String())
						} else {
							bucket.Window = win
						}
					}
					if desc, ok := bm["description"].(string); ok {
						if res := ojson.GetBytesNoCopy(respBytes, desc); res.Exists() {
							bucket.Description = strings.Clone(res.String())
						} else {
							bucket.Description = desc
						}
					}
					if reset, ok := bm["reset_time"].(string); ok {
						if res := ojson.GetBytesNoCopy(respBytes, reset); res.Exists() {
							bucket.ResetTime = strings.Clone(res.String())
						} else {
							bucket.ResetTime = reset
						}
					}
					group.Buckets = append(group.Buckets, bucket)
				}
			}
			if len(group.Buckets) > 0 {
				out.Groups = append(out.Groups, group)
			}
		}
	}

	totalBuckets := 0
	for _, g := range out.Groups {
		totalBuckets += len(g.Buckets)
	}
	hasPlan := out.Subscription != nil && strings.TrimSpace(out.Subscription.Plan) != ""
	out.Summary = filterUsableQuotaSummary(respBytes)
	hasSummary := len(out.Summary) > 0
	if totalBuckets == 0 && !hasPlan && !hasSummary {
		return out, fmt.Errorf("response mapping did not match any valid quota fields in upstream response")
	}
	return out, nil
}
