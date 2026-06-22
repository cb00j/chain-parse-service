package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, data)
}

func respondError(c *gin.Context, status int, code string, message string) {
	reqID, _ := c.Get("request_id")
	rid, _ := reqID.(string)
	c.JSON(status, errorResponse{
		Error: errorBody{Code: code, Message: message, RequestID: rid},
	})
}

func badRequest(c *gin.Context, message string) {
	respondError(c, http.StatusBadRequest, "INVALID_PARAMETER", message)
}

func notFound(c *gin.Context, message string) {
	respondError(c, http.StatusNotFound, "NOT_FOUND", message)
}

func internalError(c *gin.Context, message string) {
	respondError(c, http.StatusInternalServerError, "INTERNAL_ERROR", message)
}

func serviceUnavailable(c *gin.Context, message string) {
	respondError(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", message)
}
