package traefik_server

import (
	"database/sql"
	"encoding/json"
	"scheduling/controller/last_mile_scheduling/bpr"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

// --- Traefik Dynamic Configuration Structure Definitions ---
// (These structures are exactly the same as previously discussed)

// TraefikDynamicConfiguration The top-level structure expected by Traefik
type TraefikDynamicConfiguration struct {
	HTTP *HTTPConfiguration `json:"http,omitempty"` // omitempty: do not output if HTTP is nil
}

// HTTPConfiguration Traefik HTTP configuration
type HTTPConfiguration struct {
	Routers     map[string]Router     `json:"routers"`
	Middlewares map[string]Middleware `json:"middlewares"`
	Services    map[string]Service    `json:"services"`
}

// Router Traefik routing definition
type Router struct {
	Rule        string   `json:"rule"`
	Service     string   `json:"service"`
	EntryPoints []string `json:"entryPoints"`
	Middlewares []string `json:"middlewares,omitempty"`
}

// PluginMiddlewareConfig is the generic structure for plugin configurations,
// where the key is the plugin registration name
type PluginMiddlewareConfig map[string]interface{}

// Middleware Traefik middleware definition
type Middleware struct {
	Plugin PluginMiddlewareConfig `json:"plugin,omitempty"`
}

// Service Traefik service definition (noop-service)
type Service struct {
	LoadBalancer LoadBalancer `json:"loadBalancer"`
}

// LoadBalancer Traefik load balancer definition
type LoadBalancer struct {
	Servers []Server `json:"servers"`
}

// Server Traefik backend server definition
type Server struct {
	URL string `json:"url"`
}

// --- Custom Plugin Configuration Structure (myWeightedRedirector) ---
// (These structures are consistent with your plugin definition)

// WeightedRedirectorPluginConfig Corresponds to the configuration of the myWeightedRedirector plugin
type WeightedRedirectorPluginConfig struct {
	DefaultScheme        string        `json:"defaultScheme,omitempty"`
	DefaultPort          int           `json:"defaultPort,omitempty"`
	PermanentRedirect    bool          `json:"permanentRedirect,omitempty"`
	PreservePathAndQuery bool          `json:"preservePathAndQuery,omitempty"`
	Targets              []TargetEntry `json:"targets"` // Defines the target IPs and their weights
}

// TargetEntry Defines each target IP and its weight
type TargetEntry struct {
	IP     string `json:"ip"`     // Target IP address
	Weight int    `json:"weight"` // Corresponding weight
}

// --- Global Variables ---
var (
	currentTraefikConfig *TraefikDynamicConfiguration
	configLock           sync.RWMutex
	// The name under which the plugin is registered in Traefik,
	// must match experimental.localPlugins.<name> in traefik.yml.template
	pluginRegistrationName = "myWeightedRedirector"

	// Database connection for region lookup
	db *sql.DB
)

// SetDatabase sets the database connection for traefik_server package
func SetDatabase(database *sql.DB) {
	db = database
	log.Info("Database connection set for traefik_server")
}

// generateRegionConfig generates Traefik dynamic configuration for a specific region
// Parameters:
//   - region: Region name (e.g., "EU", "US", "Asian")
//
// Returns:
//   - *TraefikDynamicConfiguration: Configuration containing only nodes from the specified region
func generateRegionConfig(region string) *TraefikDynamicConfiguration {
	log.Infof("Generating Traefik configuration for region: %s", region)

	allBprData := bpr.GetAllBPRResults() // Get all BPR results (map[region]map[ip]weight)

	// Return empty config if no region specified
	if region == "" {
		log.Warn("No region specified, returning empty config")
		return &TraefikDynamicConfiguration{
			HTTP: &HTTPConfiguration{
				Routers:     make(map[string]Router),
				Middlewares: make(map[string]Middleware),
				Services:    make(map[string]Service),
			},
		}
	}

	// Get BPR data for the specified region
	regionBprData, exists := allBprData[region]
	if !exists || len(regionBprData) == 0 {
		log.Warnf("No BPR data found for region %s, returning empty config", region)
		return &TraefikDynamicConfiguration{
			HTTP: &HTTPConfiguration{
				Routers:     make(map[string]Router),
				Middlewares: make(map[string]Middleware),
				Services:    make(map[string]Service),
			},
		}
	}

	// Filter targets with positive weights
	targets := make([]TargetEntry, 0, len(regionBprData))
	for ip, weight := range regionBprData {
		if weight > 0 {
			targets = append(targets, TargetEntry{
				IP:     ip,
				Weight: weight,
			})
		} else {
			log.Debugf("Skipping IP %s in region %s due to non-positive weight %d", ip, region, weight)
		}
	}

	// Return empty config if all targets had non-positive weights
	if len(targets) == 0 {
		log.Warnf("All targets in region %s had non-positive weights, returning empty config", region)
		return &TraefikDynamicConfiguration{
			HTTP: &HTTPConfiguration{
				Routers:     make(map[string]Router),
				Middlewares: make(map[string]Middleware),
				Services:    make(map[string]Service),
			},
		}
	}

	// Create configuration structure
	tdc := &TraefikDynamicConfiguration{
		HTTP: &HTTPConfiguration{
			Routers:     make(map[string]Router),
			Middlewares: make(map[string]Middleware),
			Services:    make(map[string]Service),
		},
	}

	// Create a universal router for /resolve/* path (not domain-specific)
	routerName := "router-resolve-all"
	middlewareName := "weighted-redirect-" + strings.ToLower(region)
	rule := "PathPrefix(`/resolve/`)" // Match all /resolve/* requests

	pluginSpecificConfig := WeightedRedirectorPluginConfig{
		DefaultScheme:        "http",
		DefaultPort:          50055,
		PreservePathAndQuery: true,
		Targets:              targets,
	}

	tdc.HTTP.Routers[routerName] = Router{
		Rule:        rule,
		Service:     "noop-service",
		EntryPoints: []string{"web"},
		Middlewares: []string{middlewareName},
	}

	tdc.HTTP.Middlewares[middlewareName] = Middleware{
		Plugin: PluginMiddlewareConfig{
			pluginRegistrationName: pluginSpecificConfig,
		},
	}

	tdc.HTTP.Services["noop-service"] = Service{
		LoadBalancer: LoadBalancer{
			Servers: []Server{{URL: "http://127.0.0.1:1"}},
		},
	}

	d, _ := json.Marshal(tdc)
	log.Infof("Generated config for region %s with %d targets: %s", region, len(targets), string(d))

	return tdc
}
