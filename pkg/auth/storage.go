package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	storageMutex sync.RWMutex
	dataDir      = "data"
	usersFile    = "users.json"
)

const (
	defaultAdminUsername = "admin"
	defaultAdminPassword = "admin"
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

	// Initialize users file if it doesn't exist
	usersPath := filepath.Join(dataDir, usersFile)
	if _, err := os.Stat(usersPath); os.IsNotExist(err) {
		storage := &UserStorage{Users: make(map[string]*User)}
		if err := saveUsers(storage); err != nil {
			return err
		}
	}

	if err := ensureDefaultAdminUser(); err != nil {
		return err
	}

	return nil
}

// ensureDefaultAdminUser ensures admin/admin exists for admin metrics login.
func ensureDefaultAdminUser() error {
	storageMutex.Lock()
	defer storageMutex.Unlock()

	storage, err := loadUsersUnsafe()
	if err != nil {
		return fmt.Errorf("failed to load users while ensuring admin user: %v", err)
	}

	if _, exists := storage.Users[defaultAdminUsername]; exists {
		return nil
	}

	hashedPassword, err := HashPassword(defaultAdminPassword)
	if err != nil {
		return fmt.Errorf("failed to hash default admin password: %v", err)
	}

	storage.Users[defaultAdminUsername] = &User{
		Password:  hashedPassword,
		ClientID:  usernameToClientID(defaultAdminUsername),
		CreatedAt: time.Now(),
	}

	if err := saveUsers(storage); err != nil {
		return fmt.Errorf("failed to persist default admin user: %v", err)
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
