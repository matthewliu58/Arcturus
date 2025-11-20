package domain_api

import (
	"encoding/json"
	"net/http"
)

// APIResponse represents the standard API response format
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// RespondWithJSON sends a JSON response with the specified status code
func RespondWithJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false,"error":"Failed to marshal JSON response"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(response)
}

// RespondWithError sends an error response with the specified status code
func RespondWithError(w http.ResponseWriter, statusCode int, message string) {
	RespondWithJSON(w, statusCode, APIResponse{
		Success: false,
		Error:   message,
	})
}

// RespondWithSuccess sends a success response with the specified data
func RespondWithSuccess(w http.ResponseWriter, message string, data interface{}) {
	RespondWithJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}
