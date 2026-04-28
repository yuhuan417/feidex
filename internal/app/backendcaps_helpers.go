package app

import "feidex/internal/app/backendcaps"

func backendCapabilityForKind(backend string) backendcaps.CapabilitySpec {
	return backendcaps.ForKind(backend)
}
