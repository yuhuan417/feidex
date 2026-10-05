package runtime

import (
	"sync"
	"time"

	"feidex/internal/config"
	"feidex/internal/state"
)

// FrontendScope is the fully composed input for one Feishu frontend.
// internal/composition creates it; entrypoint packages only consume it.
type FrontendScope struct {
	Config          *config.Config
	ConfigPath      string
	Store           *state.Store
	ConfigMutex     *sync.RWMutex
	Frontend        config.ResolvedFrontend
	FeishuTransport any
	RuntimeOwner    *FrontendOwner
	StartedAt       time.Time
}
