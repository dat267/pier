package ai

import (
	"math"
	"time"
)

// D304: OAuthCredential stores expiry as int64 Unix milliseconds, unlike JS
// Number. Saturate values outside that range to preserve future/past status;
// exact extreme timestamps differ from upstream. See ARCHITECTURE.md.
// oauthExpiryFromLifetime applies upstream JavaScript timestamp arithmetic.
func oauthExpiryFromLifetime(expiresInSeconds, skewMS float64) int64 {
	return clampOAuthExpiryUnixMillis(float64(time.Now().UnixMilli()) + expiresInSeconds*1000 - skewMS)
}

func oauthExpiryFromUnixSeconds(expiresAtSeconds, skewMS float64) int64 {
	return clampOAuthExpiryUnixMillis(expiresAtSeconds*1000 - skewMS)
}

func clampOAuthExpiryUnixMillis(expiresAt float64) int64 {
	if math.IsNaN(expiresAt) || expiresAt >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	if expiresAt <= float64(math.MinInt64) {
		return math.MinInt64
	}
	return int64(expiresAt)
}
