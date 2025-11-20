package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

// APIClient handles communication with the controller
type APIClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewAPIClient creates a new API probing_client
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetRegionNodes fetches the list of nodes in the same region from the controller
// GET {baseURL}/api/v1/region-nodes
func (c *APIClient) GetRegionNodes() ([]NodeInfo, error) {
	url := fmt.Sprintf("%s/api/v1/region-nodes", c.baseURL)

	log.Infof("Fetching region nodes from: %s", url)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to request region nodes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var nodes []NodeInfo
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	d, _ := json.Marshal(nodes)
	log.Infof("fetched nodes from controller: %s", d)
	return nodes, nil
}

// ReportDelays sends delay measurements to the controller
// POST {baseURL}/api/v1/report-delays
func (c *APIClient) ReportDelays(payload DelayReportPayload) error {
	url := fmt.Sprintf("%s/api/v1/report-delays", c.baseURL)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	log.Infof("Reporting (%s) delay measurements to: %s", jsonData, url)

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to post delay report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	log.Infof("Successfully reported %d delay measurements", len(payload.Delays))
	return nil
}
