package common

import (
	"fmt"
	"sync"
)

// AlgorithmRegistry manages available routing algorithms
type AlgorithmRegistry struct {
	calculators map[string]PathCalculator
	mu          sync.RWMutex
}

// Global algorithm registry instance
var globalRegistry = &AlgorithmRegistry{
	calculators: make(map[string]PathCalculator),
}

// Register registers a new algorithm with the given name
func (ar *AlgorithmRegistry) Register(name string, calculator PathCalculator) error {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	if _, exists := ar.calculators[name]; exists {
		return fmt.Errorf("algorithm '%s' is already registered", name)
	}

	ar.calculators[name] = calculator
	return nil
}

// Get retrieves an algorithm by name
func (ar *AlgorithmRegistry) Get(name string) (PathCalculator, error) {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	calc, exists := ar.calculators[name]
	if !exists {
		return nil, fmt.Errorf("algorithm '%s' not found in registry", name)
	}

	return calc, nil
}

// List returns all registered algorithm names
func (ar *AlgorithmRegistry) List() []string {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	names := make([]string, 0, len(ar.calculators))
	for name := range ar.calculators {
		names = append(names, name)
	}

	return names
}

// GetGlobalRegistry returns the global algorithm registry
func GetGlobalRegistry() *AlgorithmRegistry {
	return globalRegistry
}

// RegisterGlobal registers an algorithm in the global registry
func RegisterGlobal(name string, calculator PathCalculator) error {
	return globalRegistry.Register(name, calculator)
}

// GetGlobal retrieves an algorithm from the global registry
func GetGlobal(name string) (PathCalculator, error) {
	return globalRegistry.Get(name)
}

// ListGlobal returns all registered algorithms in the global registry
func ListGlobal() []string {
	return globalRegistry.List()
}
