package common

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/samber/lo"
)

type paramOverrideAuditRecorder struct {
	lines []string
}

func shouldEnableParamOverrideAudit(paramOverride map[string]any) bool {
	if common.DebugEnabled {
		return true
	}
	if len(paramOverride) == 0 {
		return false
	}
	if operations, ok := tryParseOperations(paramOverride); ok {
		for _, operation := range operations {
			if shouldAuditParamPath(strings.TrimSpace(operation.Path)) ||
				shouldAuditParamPath(strings.TrimSpace(operation.From)) ||
				shouldAuditParamPath(strings.TrimSpace(operation.To)) {
				return true
			}
		}
		for key := range buildLegacyParamOverride(paramOverride) {
			if shouldAuditParamPath(strings.TrimSpace(key)) {
				return true
			}
		}
		return false
	}
	for key := range paramOverride {
		if shouldAuditParamPath(strings.TrimSpace(key)) {
			return true
		}
	}
	return false
}

func getParamOverrideAuditRecorder(context map[string]any) *paramOverrideAuditRecorder {
	if context == nil {
		return nil
	}
	recorder, _ := context[paramOverrideContextAuditRecorder].(*paramOverrideAuditRecorder)
	return recorder
}

func (r *paramOverrideAuditRecorder) recordOperation(mode, path, from, to string, value any) {
	if r == nil {
		return
	}
	line := buildParamOverrideAuditLine(mode, path, from, to, value)
	if line == "" {
		return
	}
	if lo.Contains(r.lines, line) {
		return
	}
	r.lines = append(r.lines, line)
}

func shouldAuditParamPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if common.DebugEnabled {
		return true
	}
	for _, prefix := range paramOverrideSensitivePathPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
}

func shouldAuditOperation(mode, path, from, to string) bool {
	if common.DebugEnabled {
		return true
	}
	return slices.ContainsFunc([]string{path, from, to}, shouldAuditParamPath)
}

func formatParamOverrideAuditValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "<empty>"
	case string:
		return typed
	default:
		return common.GetJsonString(typed)
	}
}

func buildParamOverrideAuditLine(mode, path, from, to string, value any) string {
	mode = strings.TrimSpace(mode)
	path = strings.TrimSpace(path)
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)

	if !shouldAuditOperation(mode, path, from, to) {
		return ""
	}

	switch mode {
	case "set":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("set %s = %s", path, formatParamOverrideAuditValue(value))
	case "delete":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("delete %s", path)
	case "copy":
		if from == "" || to == "" {
			return ""
		}
		return fmt.Sprintf("copy %s -> %s", from, to)
	case "move":
		if from == "" || to == "" {
			return ""
		}
		return fmt.Sprintf("move %s -> %s", from, to)
	case "prepend":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("prepend %s with %s", path, formatParamOverrideAuditValue(value))
	case "append":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("append %s with %s", path, formatParamOverrideAuditValue(value))
	case "trim_prefix", "trim_suffix", "ensure_prefix", "ensure_suffix":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("%s %s with %s", mode, path, formatParamOverrideAuditValue(value))
	case "trim_space", "to_lower", "to_upper":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("%s %s", mode, path)
	case "replace", "regex_replace":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("%s %s from %s to %s", mode, path, from, to)
	case "set_header":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("set_header %s = %s", path, formatParamOverrideAuditValue(value))
	case "delete_header":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("delete_header %s", path)
	case "copy_header", "move_header":
		if from == "" || to == "" {
			return ""
		}
		return fmt.Sprintf("%s %s -> %s", mode, from, to)
	case "pass_headers":
		return fmt.Sprintf("pass_headers %s", formatParamOverrideAuditValue(value))
	case "sync_fields":
		if from == "" || to == "" {
			return ""
		}
		return fmt.Sprintf("sync_fields %s -> %s", from, to)
	case "return_error":
		return fmt.Sprintf("return_error %s", formatParamOverrideAuditValue(value))
	default:
		if path == "" {
			return mode
		}
		return fmt.Sprintf("%s %s", mode, path)
	}
}
