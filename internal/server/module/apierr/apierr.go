// Package apierr provides a shared JSON error-response shape for module
// HTTP handlers, so packages that must avoid importing internal/server can
// depend on error-response formatting without reintroducing that cycle.
package apierr

import "github.com/gin-gonic/gin"

// Send writes a {"error": {"message", "type"}} JSON response.
func Send(c *gin.Context, status int, err error, errType string) {
	c.JSON(status, gin.H{"error": gin.H{"message": err.Error(), "type": errType}})
}
