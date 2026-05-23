package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// RunAdd creates a new empty markdown file in the specified directory.
// The filename is a randomly generated 16-character lowercase hex string.
// It retries up to 10 times in case of name collisions.
// It returns the path to the created file, or an error.
func RunAdd(dir string) (string, error) {
	// First, ensure the directory exists and is a directory.
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("failed to access directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}

	var filePath string
	var name string
	success := false

	for i := 0; i < 10; i++ {
		bytes := make([]byte, 8)
		if _, err := rand.Read(bytes); err != nil {
			return "", fmt.Errorf("failed to generate random bytes: %w", err)
		}
		name = hex.EncodeToString(bytes) + ".md"
		filePath = filepath.Join(dir, name)

		// Create file using O_CREATE and O_EXCL to ensure it doesn't overwrite an existing file.
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
		if err == nil {
			file.Close()
			success = true
			break
		}
		if !os.IsExist(err) {
			// Some other error (e.g. permission denied)
			return "", fmt.Errorf("failed to create file %s: %w", filePath, err)
		}
	}

	if !success {
		return "", fmt.Errorf("failed to create a unique markdown file after 10 attempts due to name collisions")
	}

	return filePath, nil
}
