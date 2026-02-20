package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware verifies JWT tokens and adds user info to context
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header required",
			})
			c.Abort()
			return
		}

		// Check Bearer prefix
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid authorization header format",
			})
			c.Abort()
			return
		}

		// Extract token
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		// Parse and validate token
		claims, err := ParseToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid token",
			})
			c.Abort()
			return
		}

		// Set user info in context
		c.Set("userID", claims.UserID)
		c.Set("clientID", claims.ClientID)

		c.Next()
	}
}

// OptionalAuthMiddleware allows requests with or without tokens
func OptionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No auth header, continue without user info
			c.Next()
			return
		}

		// Check Bearer prefix
		if !strings.HasPrefix(authHeader, "Bearer ") {
			// Invalid format, but don't fail
			c.Next()
			return
		}

		// Extract token
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		// Parse and validate token
		claims, err := ParseToken(tokenString)
		if err != nil {
			// Invalid token, but don't fail
			c.Next()
			return
		}

		// Set user info in context if valid
		c.Set("userID", claims.UserID)
		c.Set("clientID", claims.ClientID)

		c.Next()
	}
}

// GetUserFromContext extracts user info from gin context
func GetUserFromContext(c *gin.Context) (string, int, bool) {
	userID, exists := c.Get("userID")
	if !exists {
		return "", 0, false
	}

	clientID, exists := c.Get("clientID")
	if !exists {
		return "", 0, false
	}

	userIDStr, ok1 := userID.(string)
	clientIDInt, ok2 := clientID.(int)

	if !ok1 || !ok2 {
		return "", 0, false
	}

	return userIDStr, clientIDInt, true
}
