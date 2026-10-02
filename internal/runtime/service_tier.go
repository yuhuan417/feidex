package runtime

import "strings"

const ServiceTierFast = "fast"

func NormalizeServiceTier(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ServiceTierFast:
		return ServiceTierFast
	default:
		return ""
	}
}

func ToggleServiceTier(value string) string {
	if NormalizeServiceTier(value) == ServiceTierFast {
		return ""
	}
	return ServiceTierFast
}

func RenderServiceTierValue(value string) string {
	value = NormalizeServiceTier(value)
	if value == "" {
		return "-"
	}
	return "`" + value + "`"
}

func RenderServiceTierReplyValue(value string) string {
	value = NormalizeServiceTier(value)
	if value == "" {
		return "未设置"
	}
	return "`" + value + "`"
}
