package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// User represents a system user
type User struct {
	Password  string    `json:"password"` // bcrypt hashed password
	ClientID  int       `json:"clientId"` // DFS client ID (0 if not assigned)
	CreatedAt time.Time `json:"createdAt"`
}

// UserStorage holds all users in memory/file
type UserStorage struct {
	Users map[string]*User `json:"users"`
}

// Counter keeps track of the next user ID to assign
type Counter struct {
	NextUserID int `json:"nextUserId"`
}

// JWT Claims structure
type CustomClaims struct {
	UserID   string `json:"userId"`
	ClientID int    `json:"clientId"`
	jwt.RegisteredClaims
}

// API Request/Response structures
type RegisterRequest struct {
	UserID   string `json:"userId" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginRequest struct {
	UserID   string `json:"userId" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type AuthResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token,omitempty"`
	UserID  string `json:"userId,omitempty"`
	Message string `json:"message,omitempty"`
}
