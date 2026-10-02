package app

import "feidex/internal/application/backendcaps"

func backendCapabilityForKind(backend string) backendcaps.CapabilitySpec {
	return backendcaps.ForKind(backend)
}
