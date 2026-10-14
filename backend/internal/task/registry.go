package task

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Reporter func(Progress)

type Handler interface {
	Descriptor() Descriptor
	Prepare(context.Context, json.RawMessage) (json.RawMessage, error)
	Run(context.Context, json.RawMessage, Execution) (json.RawMessage, error)
	PublicSummary(json.RawMessage) any
}

// ScheduleIdentityProvider gives schedulable handlers a stable identity for
// definitions that must not be duplicated.
type ScheduleIdentityProvider interface {
	ScheduleIdentity(json.RawMessage) (string, bool)
}

type Registry struct {
	handlers map[string]Handler
}

func NewRegistry(handlers ...Handler) (*Registry, error) {
	registry := &Registry{handlers: make(map[string]Handler, len(handlers))}
	for _, handler := range handlers {
		if handler == nil {
			return nil, fmt.Errorf("register task handler: nil handler")
		}
		descriptor := handler.Descriptor()
		normalizedType := strings.TrimSpace(descriptor.Type)
		if normalizedType == "" {
			return nil, fmt.Errorf("register task handler: type is required")
		}
		if normalizedType != descriptor.Type {
			return nil, fmt.Errorf("register task handler %q: type must not contain surrounding whitespace", descriptor.Type)
		}
		if descriptor.PayloadVersion <= 0 {
			return nil, fmt.Errorf("register task handler %q: payload version must be positive", descriptor.Type)
		}
		if !descriptor.Manual && !descriptor.Schedulable {
			return nil, fmt.Errorf("register task handler %q: no supported trigger", descriptor.Type)
		}
		if _, exists := registry.handlers[descriptor.Type]; exists {
			return nil, fmt.Errorf("register task handler %q: duplicate type", descriptor.Type)
		}
		registry.handlers[normalizedType] = handler
	}
	return registry, nil
}

func (r *Registry) Handler(taskType string) (Handler, bool) {
	if r == nil {
		return nil, false
	}
	handler, ok := r.handlers[strings.TrimSpace(taskType)]
	return handler, ok
}

func (r *Registry) Descriptors() []Descriptor {
	if r == nil {
		return []Descriptor{}
	}
	descriptors := make([]Descriptor, 0, len(r.handlers))
	for _, handler := range r.handlers {
		descriptors = append(descriptors, handler.Descriptor())
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Type < descriptors[j].Type
	})
	return descriptors
}
