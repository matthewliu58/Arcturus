package domain_api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// DomainHandler handles HTTP requests related to domain operations
type DomainHandler struct {
	dao         DomainDAO
	fileManager FileManager
}

// FileManager defines the operations we need
type FileManager interface {
	SaveDomainIPMappings(mappings []*DomainIPMapping) error
	GetDomainIPMappings() []*DomainIPMapping
}

// DomainIPMapping is the protobuf version of domain mappings
type DomainIPMapping struct {
	Domain string
	Ip     string
}

// DomainRequest represents a request to add or update a domain
type DomainRequest struct {
	Domain   string `json:"domain"`
	OriginIP string `json:"origin_ip"`
}

// NewDomainHandler creates a new DomainHandler
func NewDomainHandler(dao DomainDAO, fileManager FileManager) *DomainHandler {
	return &DomainHandler{
		dao:         dao,
		fileManager: fileManager,
	}
}

// HandleAddDomain handles requests to add or update a domain mapping
func (h *DomainHandler) HandleAddDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req DomainRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request data format")
		return
	}
	defer r.Body.Close()

	// Validate the request
	if req.Domain == "" || req.OriginIP == "" {
		RespondWithError(w, http.StatusBadRequest, "Domain and target IP cannot be empty")
		return
	}

	// Save to database
	if err := h.dao.UpsertDomain(req.Domain, req.OriginIP); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Failed to save domain mapping: "+err.Error())
		return
	}

	// Update the configuration file
	if err := h.syncDomainConfig(); err != nil {
		// Log the error but don't fail the request
		// Database update was successful, configuration will be synced later
		RespondWithSuccess(w, "Domain mapping added, but task_dispatching sync failed: "+err.Error(), req)
		return
	}

	RespondWithSuccess(w, "Domain mapping added successfully", req)
}

// HandleDeleteDomain handles requests to delete a domain mapping
func (h *DomainHandler) HandleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		RespondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract domain from URL path or query parameter
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		// Try extracting from the path
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) > 0 {
			domain = pathParts[len(pathParts)-1]
		}
	}

	if domain == "" {
		RespondWithError(w, http.StatusBadRequest, "Domain parameter must be provided")
		return
	}

	// Delete from database
	if err := h.dao.DeleteDomain(domain); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete domain mapping: "+err.Error())
		return
	}

	// Update the configuration file
	if err := h.syncDomainConfig(); err != nil {
		// Log the error but don't fail the request
		RespondWithSuccess(w, "Domain mapping deleted, but task_dispatching sync failed: "+err.Error(), domain)
		return
	}

	RespondWithSuccess(w, "Domain mapping deleted successfully", domain)
}

// HandleListDomains handles requests to list all domain mappings
func (h *DomainHandler) HandleListDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	domains, err := h.dao.GetAllDomains()
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Failed to get domain mappings: "+err.Error())
		return
	}

	RespondWithSuccess(w, "Domain mappings retrieved successfully", domains)
}

// syncDomainConfig synchronizes domain mappings from the database to the configuration file
func (h *DomainHandler) syncDomainConfig() error {
	// Get all domains from the database
	domains, err := h.dao.GetAllDomains()
	if err != nil {
		return err
	}

	// Convert to the format expected by FileManager
	var pbMappings []*DomainIPMapping
	for _, domain := range domains {
		pbMappings = append(pbMappings, &DomainIPMapping{
			Domain: domain.Domain,
			Ip:     domain.OriginIP,
		})
	}

	// Save using FileManager
	return h.fileManager.SaveDomainIPMappings(pbMappings)
}
