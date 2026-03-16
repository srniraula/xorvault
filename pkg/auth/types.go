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

// JWT Claims structure
type CustomClaims struct {
	UserID   string `json:"userId"`
	ClientID int    `json:"clientId"`
	jwt.RegisteredClaims
}

// API Request/Response structures
type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type AuthResponse struct {
	Success  bool   `json:"success"`
	Token    string `json:"token,omitempty"`
	Username string `json:"username,omitempty"`
	Message  string `json:"message,omitempty"`
}
