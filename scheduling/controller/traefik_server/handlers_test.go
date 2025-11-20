package traefik_server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestTraefikConfigHandler(t *testing.T) {
	// Save and restore the global state currentTraefikConfig
	// Ensure tests do not interfere with each other or affect the actual package state
	originalCurrentTraefikConfig := currentTraefikConfig
	defer func() {
		currentTraefikConfig = originalCurrentTraefikConfig
	}()

	tests := []struct {
		name               string
		setupCurrentConfig *TraefikDynamicConfiguration // Used to set currentTraefikConfig before testing
		wantStatusCode     int
		wantContentType    string
		wantBody           *TraefikDynamicConfiguration // Expected response body content
		checkEmptyConfig   bool                         // Whether to specifically check for an empty configuration structure
	}{
		{
			name: "Should return valid configuration when currentTraefikConfig is set",
			setupCurrentConfig: &TraefikDynamicConfiguration{
				HTTP: &HTTPConfiguration{
					Routers: map[string]Router{
						"routing-test-1": {Rule: "Host(`example.com`)", Service: "service-test-1"},
					},
					Services: map[string]Service{
						"service-test-1": {LoadBalancer: LoadBalancer{Servers: []Server{{URL: "http://127.0.0.1:8080"}}}},
					},
				},
			},
			wantStatusCode:  http.StatusOK,
			wantContentType: "application/json",
			wantBody: &TraefikDynamicConfiguration{ // Expected response body should match setupCurrentConfig
				HTTP: &HTTPConfiguration{
					Routers: map[string]Router{
						"routing-test-1": {Rule: "Host(`example.com`)", Service: "service-test-1"},
					},
					Services: map[string]Service{
						"service-test-1": {LoadBalancer: LoadBalancer{Servers: []Server{{URL: "http://127.0.0.1:8080"}}}},
					},
				},
			},
		},
		{
			name:               "Should return valid empty configuration when currentTraefikConfig is nil",
			setupCurrentConfig: nil,
			wantStatusCode:     http.StatusOK,
			wantContentType:    "application/json",
			checkEmptyConfig:   true, // Indicates we expect a specifically structured empty configuration, not nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the global currentTraefikConfig for this test case
			// Note: Since currentTraefikConfig is a package-level variable, care must be taken with concurrent tests
			// configLock is used to protect concurrent access to currentTraefikConfig
			configLock.Lock()
			currentTraefikConfig = tt.setupCurrentConfig
			configLock.Unlock()

			// Create a mock HTTP request
			req, err := http.NewRequest("GET", "/traefik-dynamic-config", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			// Create a ResponseRecorder (type from httptest) to record the response
			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(traefikConfigHandler) // Get the handler function being tested

			// Invoke the handler with the mock request and response recorder
			handler.ServeHTTP(rr, req)

			// Check the status code
			if status := rr.Code; status != tt.wantStatusCode {
				t.Errorf("Handler returned wrong status code: got %v want %v", status, tt.wantStatusCode)
			}

			// Check the Content-Type
			if contentType := rr.Header().Get("Content-Type"); contentType != tt.wantContentType {
				t.Errorf("Handler returned wrong Content-Type: got %v want %v", contentType, tt.wantContentType)
			}

			// Parse and check the response body based on test case expectations
			var gotBody TraefikDynamicConfiguration
			if err := json.Unmarshal(rr.Body.Bytes(), &gotBody); err != nil {
				t.Fatalf("Failed to parse response body: %v, Body content: %s", err, rr.Body.String())
			}

			if tt.checkEmptyConfig {
				// For empty task_dispatching test, we expect a structure with all maps initialized but empty
				expectedEmptyBody := &TraefikDynamicConfiguration{
					HTTP: &HTTPConfiguration{
						Routers:     make(map[string]Router),
						Middlewares: make(map[string]Middleware),
						Services:    make(map[string]Service),
					},
				}
				if !reflect.DeepEqual(&gotBody, expectedEmptyBody) {
					t.Errorf("Handler returned unexpected empty configuration when task_dispatching was nil: \ngot %#v, \nexpected %#v", gotBody, expectedEmptyBody)
				}
			} else if tt.wantBody != nil {
				// For non-empty task_dispatching test, directly compare parsed structs
				if !reflect.DeepEqual(&gotBody, tt.wantBody) {
					t.Errorf("Handler returned unexpected response body: \ngot %#v, \nexpected %#v", gotBody, tt.wantBody)
				}
			}
		})
	}
}
