package config

import (
	"fmt"
	"strings"
)

type QuietMode string

const (
	QuietModeVerbose  QuietMode = "verbose"
	QuietModeProgress QuietMode = "progress"
	QuietModeNormal   QuietMode = "normal"
	QuietModeFinal    QuietMode = "final"
)

func ParseQuietMode(value QuietMode) (QuietMode, error) {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "":
		return QuietModeProgress, nil
	case string(QuietModeVerbose):
		return QuietModeVerbose, nil
	case string(QuietModeProgress):
		return QuietModeProgress, nil
	case string(QuietModeNormal):
		return QuietModeNormal, nil
	case string(QuietModeFinal):
		return QuietModeFinal, nil
	default:
		return "", fmt.Errorf("unsupported feishu.quiet %q", string(value))
	}
}

func NormalizeQuietMode(value QuietMode) (QuietMode, error) {
	return ParseQuietMode(value)
}

func (m QuietMode) String() string {
	normalized, err := ParseQuietMode(m)
	if err != nil {
		return string(QuietModeProgress)
	}
	return string(normalized)
}

func (m *QuietMode) UnmarshalTOML(value any) error {
	if m == nil {
		return fmt.Errorf("nil quiet mode")
	}
	switch x := value.(type) {
	case string:
		normalized, err := ParseQuietMode(QuietMode(x))
		if err != nil {
			*m = QuietModeNormal
			return nil
		}
		*m = normalized
		return nil
	default:
		*m = QuietModeNormal
		return nil
	}
}
