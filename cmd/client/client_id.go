package main

import (
	"log"
	"os"
	"strconv"
	"strings"
)

const clientIDFile = ".client_id"

// loadClientID reads the client ID from .client_id file
// Returns 0 if file doesn't exist (new client)
func loadClientID() int64 {
	data, err := os.ReadFile(clientIDFile)
	if err != nil {
		// File doesn't exist - new client
		return 0
	}

	idStr := strings.TrimSpace(string(data))
	clientID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		log.Printf("Warning: Invalid client ID in %s, treating as new client", clientIDFile)
		return 0
	}

	return clientID
}

// saveClientID saves the client ID to .client_id file
func saveClientID(clientID int64) error {
	data := strconv.FormatInt(clientID, 10)
	err := os.WriteFile(clientIDFile, []byte(data), 0644)
	if err != nil {
		return err
	}
	log.Printf("Saved client ID: %d", clientID)
	return nil
}
