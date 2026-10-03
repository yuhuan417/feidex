package modelconfig

import (
	"strings"

	"feidex/internal/domain/modelconfig"
)

// DefaultModelEntry returns the backend-declared default model, falling back
// to the first catalog entry when the backend omits the default marker.
func DefaultModelEntry(result modelconfig.ModelListResult) *modelconfig.ModelListEntry {
	for i := range result.Data {
		if result.Data[i].IsDefault {
			return &result.Data[i]
		}
	}
	if len(result.Data) == 0 {
		return nil
	}
	return &result.Data[0]
}

// LookupModelEntry finds a model by its protocol ID or display model name.
func LookupModelEntry(result modelconfig.ModelListResult, modelID string) *modelconfig.ModelListEntry {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return DefaultModelEntry(result)
	}
	for i := range result.Data {
		if result.Data[i].ID == modelID || result.Data[i].Model == modelID {
			return &result.Data[i]
		}
	}
	return nil
}

// FindModelEntry resolves a requested model and falls back to the catalog
// default when the request is empty or stale.
func FindModelEntry(result modelconfig.ModelListResult, modelID string) *modelconfig.ModelListEntry {
	if found := LookupModelEntry(result, modelID); found != nil {
		return found
	}
	return DefaultModelEntry(result)
}

// ModelSupportsEffort reports whether a model advertises a requested effort.
func ModelSupportsEffort(model *modelconfig.ModelListEntry, effort string) bool {
	effort = strings.TrimSpace(effort)
	if model == nil || effort == "" {
		return true
	}
	for _, item := range model.SupportedReasoningEfforts {
		if strings.TrimSpace(item.ReasoningEffort) == effort {
			return true
		}
	}
	return false
}
