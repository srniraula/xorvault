package auth

import (
	"hash/fnv"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// usernameToClientID derives a stable numeric DFS client ID from a username
// using FNV-1a hashing so the same user always maps to the same client ID.
func usernameToClientID(username string) int {
	h := fnv.New32a()
	h.Write([]byte(username))
	return int(h.Sum32())
}

// RegisterHandler handles user registration
func RegisterHandler(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, AuthResponse{
			Success: false,
			Message: "Invalid request format",
		})
		return
	}

	// Validate username
	if err := ValidateUsername(req.Username); err != nil {
		c.JSON(http.StatusBadRequest, AuthResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// Check if username is already taken
	if _, err := GetUser(req.Username); err == nil {
		c.JSON(http.StatusConflict, AuthResponse{
			Success: false,
			Message: "Username already taken",
		})
		return
	}

	// Validate password
	if err := ValidatePassword(req.Password); err != nil {
		c.JSON(http.StatusBadRequest, AuthResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// Hash password
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, AuthResponse{
			Success: false,
			Message: "Failed to process password",
		})
		return
	}

	// Derive a stable clientID from the username so it is always non-zero and
	// consistent across restarts without needing a separate assignment step.
	clientID := usernameToClientID(req.Username)

	// Create user
	user := &User{
		Password:  hashedPassword,
		ClientID:  clientID,
		CreatedAt: time.Now(),
	}

	// Save user
	if err := SaveUser(req.Username, user); err != nil {
		c.JSON(http.StatusInternalServerError, AuthResponse{
			Success: false,
			Message: "Failed to create user",
		})
		return
	}

	// Generate JWT token (clientID is already set)
	token, err := GenerateToken(req.Username, clientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, AuthResponse{
			Success: false,
			Message: "Failed to generate token",
		})
		return
	}

	c.JSON(http.StatusCreated, AuthResponse{
		Success:  true,
		Token:    token,
		Username: req.Username,
		Message:  "Registration successful",
	})
}

// LoginHandler handles user login
func LoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, AuthResponse{
			Success: false,
			Message: "Invalid request format",
		})
		return
	}

	// Validate input
	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, AuthResponse{
			Success: false,
			Message: "Username and password are required",
		})
		return
	}

	// Get user from storage
	user, err := GetUser(req.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, AuthResponse{
			Success: false,
			Message: "Username or password is incorrect",
		})
		return
	}

	// Check password
	if !CheckPasswordHash(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, AuthResponse{
			Success: false,
			Message: "Username or password is incorrect",
		})
		return
	}

	// Generate JWT token
	token, err := GenerateToken(req.Username, user.ClientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, AuthResponse{
			Success: false,
			Message: "Failed to generate token",
		})
		return
	}

	c.JSON(http.StatusOK, AuthResponse{
		Success:  true,
		Token:    token,
		Username: req.Username,
		Message:  "Login successful",
	})
}
