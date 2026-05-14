package main

import (
	"fmt"
	"log"
	"dfs-project/pkg/auth"
)

// Quick utility to generate a bcrypt hash for a new password
func main() {
	// Change this to your desired new password
	newPassword := "YourNewPassword123!"

	// Validate password format
	if err := auth.ValidatePassword(newPassword); err != nil {
		log.Fatalf("Password validation failed: %v", err)
	}

	// Generate bcrypt hash
	hashedPassword, err := auth.HashPassword(newPassword)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	fmt.Println("New password hash for admin:")
	fmt.Println(hashedPassword)
	fmt.Println("\nUpdate data/users.json with this hash in the 'password' field")
}
