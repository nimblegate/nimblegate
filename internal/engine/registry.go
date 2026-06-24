// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"

	"nimblegate/internal/frames"
)

// RegisteredFrame is a Frame plus its bound CheckFunc.
type RegisteredFrame struct {
	Frame frames.Frame
	Check CheckFunc
}

// Registry is the flat in-memory store of all enabled frames, indexed by trigger.
type Registry struct {
	all       map[string]RegisteredFrame
	byTrigger map[string][]RegisteredFrame
}

func NewRegistry() *Registry {
	return &Registry{
		all:       map[string]RegisteredFrame{},
		byTrigger: map[string][]RegisteredFrame{},
	}
}

// Add inserts a frame. Returns an error if the frame's ID is already present.
func (r *Registry) Add(f frames.Frame, check CheckFunc) error {
	id := f.ID()
	if _, exists := r.all[id]; exists {
		return fmt.Errorf("registry: duplicate frame id %q (existing from %s, new from %s)",
			id, r.all[id].Frame.SourcePath, f.SourcePath)
	}
	rf := RegisteredFrame{Frame: f, Check: check}
	r.all[id] = rf
	r.indexTriggers(rf)
	return nil
}

// AddProjectOverride replaces a stdlib frame with a project-local frame of the
// same ID. If no stdlib frame exists for that ID, behaves like Add.
func (r *Registry) AddProjectOverride(f frames.Frame, check CheckFunc) error {
	id := f.ID()
	if _, exists := r.all[id]; exists {
		r.removeFromTriggers(id)
		delete(r.all, id)
	}
	return r.Add(f, check)
}

// MatchingTrigger returns all registered frames whose Triggers list contains trigger.
func (r *Registry) MatchingTrigger(trigger string) []RegisteredFrame {
	return r.byTrigger[trigger]
}

// All returns every registered frame.
func (r *Registry) All() []RegisteredFrame {
	out := make([]RegisteredFrame, 0, len(r.all))
	for _, rf := range r.all {
		out = append(out, rf)
	}
	return out
}

// Get returns a frame by its ID.
func (r *Registry) Get(id string) (RegisteredFrame, bool) {
	rf, ok := r.all[id]
	return rf, ok
}

func (r *Registry) indexTriggers(rf RegisteredFrame) {
	for _, t := range rf.Frame.Frontmatter.Triggers {
		r.byTrigger[t] = append(r.byTrigger[t], rf)
	}
}

func (r *Registry) removeFromTriggers(id string) {
	for trig, list := range r.byTrigger {
		filtered := list[:0]
		for _, rf := range list {
			if rf.Frame.ID() != id {
				filtered = append(filtered, rf)
			}
		}
		r.byTrigger[trig] = filtered
	}
}
