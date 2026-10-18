package common

import (
	"strings"

	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/tidwall/gjson"
)

func syncReasoningEffortAfterParamOverride(info *RelayInfo, before, after []byte) {
	if info == nil {
		return
	}
	_, existedBefore := extractReasoningEffortFromJSON(info.GetFinalRequestRelayFormat(), before)
	effort, existsAfter := extractReasoningEffortFromJSON(info.GetFinalRequestRelayFormat(), after)
	if existsAfter {
		info.SetReasoningEffort(effort)
		return
	}
	if existedBefore {
		info.SetReasoningEffort("")
	}
}

func extractReasoningEffortFromJSON(format types.RelayFormat, data []byte) (string, bool) {
	switch format {
	case types.RelayFormatOpenAI:
		if effort, exists := firstStringValue(data, "reasoning_effort"); exists && effort != "" {
			return effort, true
		}
		if enabled := gjson.GetBytes(data, "reasoning.enabled"); enabled.Exists() {
			if enabled.Type != gjson.True && enabled.Type != gjson.False {
				return "", true
			}
			if !enabled.Bool() {
				return string(kitreasoning.EffortNone), true
			}
			if effort, exists := firstStringValue(data, "reasoning.effort"); exists && effort != "" {
				return effort, true
			}
			if budget := gjson.GetBytes(data, "reasoning.max_tokens"); budget.Exists() {
				return reasoningEffortFromBudgetValue(budget)
			}
			return string(kitreasoning.EffortHigh), true
		}
		if effort, exists := firstStringValue(data, "reasoning.effort"); exists && effort != "" {
			return effort, true
		}
		if budget := gjson.GetBytes(data, "reasoning.max_tokens"); budget.Exists() {
			return reasoningEffortFromBudgetValue(budget)
		}
		return "", false
	case types.RelayFormatOpenAIResponses:
		return firstStringValue(data, "reasoning.effort")
	case types.RelayFormatClaude:
		if effort, exists := firstStringValue(data, "output_config.effort"); exists && effort != "" {
			return effort, true
		}
		thinkingType, hasThinkingType := firstStringValue(data, "thinking.type")
		if thinkingType == "disabled" {
			return string(kitreasoning.EffortNone), true
		}
		if budget := gjson.GetBytes(data, "thinking.budget_tokens"); budget.Exists() {
			return reasoningEffortFromBudgetValue(budget)
		}
		if thinkingType == "enabled" || thinkingType == "adaptive" {
			return string(kitreasoning.EffortHigh), true
		}
		return "", hasThinkingType
	case types.RelayFormatGemini:
		level, hasLevel := firstStringValue(data,
			"generationConfig.thinkingConfig.thinkingLevel",
			"generation_config.thinking_config.thinking_level",
		)
		if level != "" {
			return level, true
		}
		for _, path := range []string{
			"generationConfig.thinkingConfig.thinkingBudget",
			"generation_config.thinking_config.thinking_budget",
		} {
			if budget := gjson.GetBytes(data, path); budget.Exists() {
				return reasoningEffortFromBudgetValue(budget)
			}
		}
		return "", hasLevel
	default:
		return "", false
	}
}

func firstStringValue(data []byte, paths ...string) (string, bool) {
	for _, path := range paths {
		value := gjson.GetBytes(data, path)
		if !value.Exists() {
			continue
		}
		if value.Type != gjson.String {
			return "", true
		}
		return strings.TrimSpace(value.String()), true
	}
	return "", false
}

func reasoningEffortFromBudgetValue(value gjson.Result) (string, bool) {
	if value.Type != gjson.Number {
		return "", true
	}
	budget := value.Float()
	switch {
	case budget == 0:
		return string(kitreasoning.EffortNone), true
	case budget < 0:
		return string(kitreasoning.EffortHigh), true
	case budget <= 1024:
		return string(kitreasoning.EffortLow), true
	case budget <= 8192:
		return string(kitreasoning.EffortMedium), true
	default:
		return string(kitreasoning.EffortHigh), true
	}
}
