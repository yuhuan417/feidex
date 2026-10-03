package runtime

import "sync"

// Registry stores frontend-scoped composition state. It is created by the
// composition root and injected into the Feishu entrypoint.
type Registry struct {
	Mu          sync.Mutex
	WorkspaceMu sync.Mutex
	ClientsMu   sync.RWMutex

	FeishuTransport any
	Codex           any
	Claude          any

	values map[string]any
}

func NewRegistry(feishuTransport any) *Registry {
	return &Registry{FeishuTransport: feishuTransport, values: make(map[string]any)}
}

func (r *Registry) Get(key string) any {
	if r == nil {
		return nil
	}
	r.Mu.Lock()
	defer r.Mu.Unlock()
	return r.values[key]
}

func (r *Registry) Set(key string, value any) {
	if r == nil {
		return
	}
	r.Mu.Lock()
	defer r.Mu.Unlock()
	if r.values == nil {
		r.values = make(map[string]any)
	}
	if value == nil {
		delete(r.values, key)
		return
	}
	r.values[key] = value
}

func (r *Registry) Delete(key string) { r.Set(key, nil) }
