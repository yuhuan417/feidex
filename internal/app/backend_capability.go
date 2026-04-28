package app

import "feidex/internal/app/backendcaps"

func backendCommandPolicy(backend string, policy localCommandBackendSpec) map[string]localCommandBackendSpec {
	backend = normalizeRuntimeBackend(backend)
	if backend == "" {
		return nil
	}
	return map[string]localCommandBackendSpec{backend: policy}
}

func backendPoliciesForUnsupportedFeature(feature backendFeature) map[string]localCommandBackendSpec {
	policies := map[string]localCommandBackendSpec{}
	for _, backend := range backendcaps.Kinds() {
		if backendcaps.ForKind(backend).Supports(feature) {
			continue
		}
		policies[backend] = hiddenBackendCommand()
	}
	if len(policies) == 0 {
		return nil
	}
	return policies
}
