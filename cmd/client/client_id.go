package main

import (
	"log"
	"os"
	"strings"
)

const usernameFile = ".username"

// loadUsername reads the username from .username file
// Returns empty string if file doesn't exist (new user)
func loadUsername() string {
	data, err := os.ReadFile(usernameFile)
	if err != nil {
		// File doesn't exist - new user
		return ""
	}

	return strings.TrimSpace(string(data))
}

// saveUsername saves the username to .username file
func saveUsername(username string) error {
	err := os.WriteFile(usernameFile, []byte(username), 0644)
	if err != nil {
		return err
	}
	log.Printf("Saved username: %s", username)
	return nil
}
