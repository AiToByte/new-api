package relay

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// SettleUsage dispatches post-consume settlement for one completed text/audio
// attempt: usage carrying audio tokens settles via PostAudioConsumeQuota when
// the model has audio ratios configured, everything else via
// PostTextConsumeQuota. It replaces the identical predicate + branch
// previously copied across the compatible/audio handlers.
//
// Deliberately NOT applied to call sites that settle unconditionally via
// PostTextConsumeQuota today (embedding, rerank, image, gemini/claude text
// paths, alpha_search): routing audio-token-bearing usage to
// PostAudioConsumeQuota there would change settlement amounts, which belongs
// to Phase 2 under the billing rules gate — not to a mechanical extraction.
func SettleUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	containAudioTokens := usage.CompletionTokenDetails.AudioTokens > 0 || usage.PromptTokensDetails.AudioTokens > 0
	containsAudioRatios := ratio_setting.ContainsAudioRatio(info.OriginModelName) || ratio_setting.ContainsAudioCompletionRatio(info.OriginModelName)

	if containAudioTokens && containsAudioRatios {
		service.PostAudioConsumeQuota(c, info, usage, nil)
	} else {
		service.PostTextConsumeQuota(c, info, usage, nil)
	}
}
