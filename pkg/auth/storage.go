package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	storageMutex sync.RWMutex
	dataDir      = "data"
	usersFile    = "users.json"
	counterFile  = "counter.json"
)

// SetStorageDir sets the base directory for user data storage
func SetStorageDir(dir string) {
	storageMutex.Lock()
	defer storageMutex.Unlock()
	dataDir = dir
}

// InitStorage creates data directory and initializes files
func InitStorage() error {
	// Create data directory
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %v", err)
	}

	// Initialize counter if it doesn't exist
	counterPath := filepath.Join(dataDir, counterFile)
	if _, err := os.Stat(counterPath); os.IsNotExist(err) {
		counter := &Counter{NextUserID: 100000}
		if err := saveCounter(counter); err != nil {
			return err
		}
	}

	// Initialize users file if it doesn't exist
	usersPath := filepath.Join(dataDir, usersFile)
	if _, err := os.Stat(usersPath); os.IsNotExist(err) {
		storage := &UserStorage{Users: make(map[string]*User)}
		if err := saveUsers(storage); err != nil {
			return err
		}
	}

	return nil
}

// LoadUsers loads all users from file
func LoadUsers() (*UserStorage, error) {
	storageMutex.RLock()
	defer storageMutex.RUnlock()

	usersPath := filepath.Join(dataDir, usersFile)
	data, err := os.ReadFile(usersPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read users file: %v", err)
	}

	var storage UserStorage
	if err := json.Unmarshal(data, &storage); err != nil {
		return nil, fmt.Errorf("failed to parse users file: %v", err)
	}

	if storage.Users == nil {
		storage.Users = make(map[string]*User)
	}

	return &storage, nil
}

// SaveUsers saves all users to file
func saveUsers(storage *UserStorage) error {
	data, err := json.MarshalIndent(storage, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal users: %v", err)
	}

	usersPath := filepath.Join(dataDir, usersFile)
	if err := os.WriteFile(usersPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write users file: %v", err)
	}

	return nil
}

// SaveUser saves a single user to storage
func SaveUser(userID string, user *User) error {
	storageMutex.Lock()
	defer storageMutex.Unlock()

	storage, err := loadUsersUnsafe()
	if err != nil {
		return err
	}

	storage.Users[userID] = user
	return saveUsers(storage)
}

// GetUser retrieves a user from storage
func GetUser(userID string) (*User, error) {
	storageMutex.RLock()
	defer storageMutex.RUnlock()

	storage, err := loadUsersUnsafe()
	if err != nil {
		return nil, err
	}

	user, exists := storage.Users[userID]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	return user, nil
}

// UpdateUserClientID updates the clientID for a user
func UpdateUserClientID(userID string, clientID int) error {
	storageMutex.Lock()
	defer storageMutex.Unlock()

	storage, err := loadUsersUnsafe()
	if err != nil {
		return err
	}

	user, exists := storage.Users[userID]
	if !exists {
		return fmt.Errorf("user not found")
	}

	user.ClientID = clientID
	return saveUsers(storage)
}

// GenerateUserID generates a new 6-digit userID
func GenerateUserID() (string, error) {
	storageMutex.Lock()
	defer storageMutex.Unlock()

	counter, err := loadCounterUnsafe()
	if err != nil {
		return "", err
	}

	userID := fmt.Sprintf("%06d", counter.NextUserID)
	counter.NextUserID++

	if err := saveCounter(counter); err != nil {
		return "", err
	}

	return userID, nil
}

// loadUsersUnsafe loads users without mutex (caller must handle locking)
func loadUsersUnsafe() (*UserStorage, error) {
	usersPath := filepath.Join(dataDir, usersFile)
	data, err := os.ReadFile(usersPath)
	if err != nil {
		return nil, err
	}

	var storage UserStorage
	if err := json.Unmarshal(data, &storage); err != nil {
		return nil, err
	}

	if storage.Users == nil {
		storage.Users = make(map[string]*User)
	}

	return &storage, nil
}

// loadCounterUnsafe loads counter without mutex
func loadCounterUnsafe() (*Counter, error) {
	counterPath := filepath.Join(dataDir, counterFile)
	data, err := os.ReadFile(counterPath)
	if err != nil {
		return nil, err
	}

	var counter Counter
	if err := json.Unmarshal(data, &counter); err != nil {
		return nil, err
	}

	return &counter, nil
}

// saveCounter saves counter to file
func saveCounter(counter *Counter) error {
	data, err := json.MarshalIndent(counter, "", "  ")
	if err != nil {
		return err
	}

	counterPath := filepath.Join(dataDir, counterFile)
	return os.WriteFile(counterPath, data, 0600)
}
