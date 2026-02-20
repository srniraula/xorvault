package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

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

	// Validate userID
	if err := ValidateUserID(req.UserID); err != nil {
		c.JSON(http.StatusBadRequest, AuthResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// Check if userID already exists
	if _, err := GetUser(req.UserID); err == nil {
		c.JSON(http.StatusConflict, AuthResponse{
			Success: false,
			Message: "User ID already exists",
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

	// Create user
	user := &User{
		Password:  hashedPassword,
		ClientID:  0, // Will be assigned on first upload
		CreatedAt: time.Now(),
	}

	// Save user
	if err := SaveUser(req.UserID, user); err != nil {
		c.JSON(http.StatusInternalServerError, AuthResponse{
			Success: false,
			Message: "Failed to create user",
		})
		return
	}

	// Generate JWT token
	token, err := GenerateToken(req.UserID, user.ClientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, AuthResponse{
			Success: false,
			Message: "Failed to generate token",
		})
		return
	}

	c.JSON(http.StatusCreated, AuthResponse{
		Success: true,
		Token:   token,
		UserID:  req.UserID,
		Message: "Registration successful",
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
	if req.UserID == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, AuthResponse{
			Success: false,
			Message: "UserID and password are required",
		})
		return
	}

	// Get user from storage
	user, err := GetUser(req.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, AuthResponse{
			Success: false,
			Message: "UserID or password not registered",
		})
		return
	}

	// Check password
	if !CheckPasswordHash(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, AuthResponse{
			Success: false,
			Message: "UserID or password not registered",
		})
		return
	}

	// Generate JWT token
	token, err := GenerateToken(req.UserID, user.ClientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, AuthResponse{
			Success: false,
			Message: "Failed to generate token",
		})
		return
	}

	c.JSON(http.StatusOK, AuthResponse{
		Success: true,
		Token:   token,
		UserID:  req.UserID,
		Message: "Login successful",
	})
}
