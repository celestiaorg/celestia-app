package server

import (
	"sync"
	"time"
)

// TargetEntry represents a Prometheus scrape target stored in the registry.
type TargetEntry struct {
	NodeID       string
	Address      string
	Labels       map[string]string
	RegisteredAt time.Time
}

// Registry manages the collection of scrape targets.
type Registry struct {
	mu      sync.RWMutex
	targets map[string]*TargetEntry

	// onChange is called whenever the registry changes.
	onChange func()
}

// NewRegistry creates a new target registry.
func NewRegistry(onChange func()) *Registry {
	return &Registry{
		targets:  make(map[string]*TargetEntry),
		onChange: onChange,
	}
}

// Register adds or updates a target in the registry.
func (r *Registry) Register(nodeID, address string, labels map[string]string) {
	r.mu.Lock()
	r.targets[nodeID] = &TargetEntry{
		NodeID:       nodeID,
		Address:      address,
		Labels:       labels,
		RegisteredAt: time.Now(),
	}
	r.mu.Unlock()

	// Call onChange outside the lock to prevent deadlock
	if r.onChange != nil {
		r.onChange()
	}
}

// Deregister removes a target from the registry.
func (r *Registry) Deregister(nodeID string) bool {
	r.mu.Lock()
	if _, exists := r.targets[nodeID]; !exists {
		r.mu.Unlock()
		return false
	}

	delete(r.targets, nodeID)
	r.mu.Unlock()

	// Call onChange outside the lock to prevent deadlock
	if r.onChange != nil {
		r.onChange()
	}

	return true
}

// List returns all registered targets.
func (r *Registry) List() []*TargetEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*TargetEntry, 0, len(r.targets))
	for _, t := range r.targets {
		// Return a copy to prevent external modification
		entry := &TargetEntry{
			NodeID:       t.NodeID,
			Address:      t.Address,
			Labels:       make(map[string]string, len(t.Labels)),
			RegisteredAt: t.RegisteredAt,
		}
		for k, v := range t.Labels {
			entry.Labels[k] = v
		}
		result = append(result, entry)
	}
	return result
}

// Count returns the number of registered targets.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.targets)
}
