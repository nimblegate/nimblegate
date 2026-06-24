// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package upstream

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// Registry holds the set of registered adapters + URL-to-adapter mappings.
// Maps host (e.g. "github.com") OR explicit URL prefix to adapter name.
type Registry struct {
	mu       sync.RWMutex
	byName   map[string]Upstream
	byHost   map[string]string // host → adapter name
	override map[string]string // URL prefix → adapter name
}

func NewRegistry() *Registry {
	return &Registry{
		byName:   map[string]Upstream{},
		byHost:   map[string]string{},
		override: map[string]string{},
	}
}

func (r *Registry) Register(name string, u Upstream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName[name] = u
}

func (r *Registry) RegisterHost(host, adapterName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byHost[host] = adapterName
}

func (r *Registry) RegisterOverride(urlPrefix, adapterName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.override[urlPrefix] = adapterName
}

func (r *Registry) LookupByURL(upstreamURL string) (Upstream, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for prefix, name := range r.override {
		if strings.HasPrefix(upstreamURL, prefix) {
			if u, ok := r.byName[name]; ok {
				return u, nil
			}
		}
	}

	u, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("parse upstream URL: %w", err)
	}
	if name, ok := r.byHost[u.Host]; ok {
		if adapter, ok := r.byName[name]; ok {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("no upstream adapter for host %s (registered: %v)", u.Host, r.knownHosts())
}

func (r *Registry) knownHosts() []string {
	out := make([]string, 0, len(r.byHost))
	for h := range r.byHost {
		out = append(out, h)
	}
	return out
}
