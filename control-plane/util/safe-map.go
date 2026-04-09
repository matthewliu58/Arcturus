package util

import (
	"errors"
	"sync"
)

// SafeMap is a concurrency-safe map[string]interface{}.
type SafeMap struct {
	mu sync.RWMutex
	m  map[string]interface{}
}

// NewSafeMap creates a new SafeMap.
func NewSafeMap() *SafeMap {
	return &SafeMap{
		m: make(map[string]interface{}),
	}
}

// Get returns the value for a key.
func (s *SafeMap) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	return v, ok
}

// Set sets the value for a key.
func (s *SafeMap) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
}

// Delete removes a key.
func (s *SafeMap) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
}

// Len returns the number of entries.
func (s *SafeMap) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

// Range iterates over all entries.
// If fn returns false, iteration stops.
func (s *SafeMap) Range(fn func(key string, value interface{}) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for k, v := range s.m {
		if !fn(k, v) {
			return
		}
	}
}

// GetAll returns a snapshot copy of the map.
func (s *SafeMap) GetAll() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make(map[string]interface{}, len(s.m))
	for k, v := range s.m {
		res[k] = v
	}
	return res
}

// Update updates a value in place under write lock.
// This is the ONLY safe way to mutate stored objects.
func (s *SafeMap) Update(
	key string,
	fn func(value interface{}) error,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.m[key]
	if !ok {
		return errors.New("key not found")
	}
	return fn(v)
}
