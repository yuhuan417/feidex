package appcore

import "context"

// Context supplies the host lifecycle to services without widening every
// capability interface. Standalone hosts may omit the lifecycle capability.
func Context(host any) context.Context {
	if provider, ok := host.(interface{ Context() context.Context }); ok {
		if ctx := provider.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}
