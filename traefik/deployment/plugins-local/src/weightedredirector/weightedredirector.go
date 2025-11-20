package weightedredirector // <--- package name

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"time"
)

// TargetEntry defines each target IP and its weight
type TargetEntry struct {
	IP     string `json:"ip,omitempty" yaml:"ip,omitempty" toml:"ip,omitempty"`
	Weight int    `json:"weight,omitempty" yaml:"weight,omitempty" toml:"weight,omitempty"`
}

// Config is the plugin's configuration structure
type Config struct {
	Targets              []TargetEntry `json:"targets,omitempty" yaml:"targets,omitempty" toml:"targets,omitempty"`
	DefaultScheme        string        `json:"defaultScheme,omitempty" yaml:"defaultScheme,omitempty" toml:"defaultScheme,omitempty"`
	DefaultPort          int           `json:"defaultPort,omitempty" yaml:"defaultPort,omitempty" toml:"defaultPort,omitempty"`
	PermanentRedirect    bool          `json:"permanentRedirect,omitempty" yaml:"permanentRedirect,omitempty" toml:"permanentRedirect,omitempty"`
	PreservePathAndQuery bool          `json:"preservePathAndQuery,omitempty" yaml:"preservePathAndQuery,omitempty" toml:"preservePathAndQuery,omitempty"`
}

// CreateConfig creates the plugin's default configuration
func CreateConfig() *Config {
	return &Config{
		Targets:              []TargetEntry{},
		DefaultScheme:        "http",
		DefaultPort:          80,
		PermanentRedirect:    false,
		PreservePathAndQuery: false,
	}
}

// WeightedRedirector plugin structure
type WeightedRedirector struct {
	next                 http.Handler
	config               *Config
	name                 string
	totalWeight          int
	targetsWithCumWeight []struct {
		TargetEntry
		cumulativeWeight int
	}
	random *rand.Rand
}

// New creates the plugin instance
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if len(config.Targets) == 0 {
		return nil, fmt.Errorf("plugin %s: targets cannot be empty", name)
	}

	plugin := &WeightedRedirector{
		next:   next,
		config: config,
		name:   name,
		random: rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	currentCumulativeWeight := 0
	for _, target := range config.Targets {
		if target.Weight <= 0 {
			return nil, fmt.Errorf("plugin %s: target weight must be positive for IP %s", name, target.IP)
		}
		if target.IP == "" {
			return nil, fmt.Errorf("plugin %s: target IP cannot be empty", name)
		}
		plugin.totalWeight += target.Weight
		currentCumulativeWeight += target.Weight
		plugin.targetsWithCumWeight = append(plugin.targetsWithCumWeight, struct {
			TargetEntry
			cumulativeWeight int
		}{target, currentCumulativeWeight})
	}

	if plugin.totalWeight == 0 {
		return nil, fmt.Errorf("plugin %s: total weight of targets is zero", name)
	}
	return plugin, nil
}

// ServeHTTP handles requests
func (w *WeightedRedirector) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if w.totalWeight == 0 || len(w.config.Targets) == 0 {
		w.next.ServeHTTP(rw, req)
		return
	}

	randomPick := w.random.Intn(w.totalWeight)

	var selectedIP string
	for _, t := range w.targetsWithCumWeight {
		if randomPick < t.cumulativeWeight {
			selectedIP = t.IP
			break
		}
	}

	if selectedIP == "" {
		// As a fallback, if for some reason no IP was selected (shouldn't happen if totalWeight > 0)
		// or if the target list was cleared after initial setup (though plugin task_dispatching is usually immutable).
		if len(w.config.Targets) > 0 {
			selectedIP = w.config.Targets[0].IP // Select the first as a default
		} else {
			// If no targets are available, pass the request to the next handler.
			w.next.ServeHTTP(rw, req)
			return
		}
	}

	targetURLVal := url.URL{
		Scheme: w.config.DefaultScheme,
		Host:   selectedIP,
	}
	// Only add port if it's not the default port for its protocol
	if w.config.DefaultPort > 0 &&
		!((w.config.DefaultScheme == "http" && w.config.DefaultPort == 80) ||
			(w.config.DefaultScheme == "https" && w.config.DefaultPort == 443)) {
		targetURLVal.Host = fmt.Sprintf("%s:%d", selectedIP, w.config.DefaultPort)
	}

	if w.config.PreservePathAndQuery {
		targetURLVal.Path = req.URL.Path
		targetURLVal.RawQuery = req.URL.RawQuery
	} else {
		targetURLVal.Path = "/" // Defaults to redirecting to the root path
	}

	finalRedirectURL := targetURLVal.String()
	statusCode := http.StatusFound // 302 Temporary Redirect
	if w.config.PermanentRedirect {
		statusCode = http.StatusMovedPermanently // 301 Permanent Redirect
	}
	http.Redirect(rw, req, finalRedirectURL, statusCode)
}
