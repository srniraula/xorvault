package auth

import (
	"fmt"
	"regexp"

	"golang.org/x/crypto/bcrypt"
)

// ValidateUsername checks if a username meets requirements.
// Requirements: 3-20 characters, must start with a letter, letters/numbers/underscores only.
func ValidateUsername(username string) error {
	if len(username) < 3 {
		return fmt.Errorf("username must be at least 3 characters long")
	}

	if len(username) > 20 {
		return fmt.Errorf("username must be at most 20 characters long")
	}

	// Must start with a letter
	if !regexp.MustCompile(`^[a-zA-Z]`).MatchString(username) {
		return fmt.Errorf("username must start with a letter")
	}

	// Only letters, numbers, and underscores allowed
	if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`).MatchString(username) {
		return fmt.Errorf("username can only contain letters, numbers, and underscores")
	}

	return nil
}

// ValidatePassword checks if password meets requirements
// Requirements: minimum length 6, at least one letter, one number, one special character
func ValidatePassword(password string) error {
	if len(password) < 6 {
		return fmt.Errorf("password must be at least 6 characters long")
	}

	hasLetter := regexp.MustCompile(`[a-zA-Z]`).MatchString(password)
	hasNumber := regexp.MustCompile(`[0-9]`).MatchString(password)
	hasSpecial := regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?]`).MatchString(password)

	if !hasLetter {
		return fmt.Errorf("password must contain at least one letter")
	}

	if !hasNumber {
		return fmt.Errorf("password must contain at least one number")
	}

	if !hasSpecial {
		return fmt.Errorf("password must contain at least one special character")
	}

	return nil
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %v", err)
	}
	return string(bytes), nil
}

// CheckPasswordHash compares a password with its hash
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
