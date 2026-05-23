package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAdd_Success(t *testing.T) {
	tempDir := t.TempDir()

	filePath, err := RunAdd(tempDir)
	if err != nil {
		t.Fatalf("RunAdd failed: %v", err)
	}

	// Verify file path is in the temp directory
	if !strings.HasPrefix(filePath, tempDir) {
		t.Errorf("expected file path to be in %s, got %s", tempDir, filePath)
	}

	// Verify file name constraints
	fileName := filepath.Base(filePath)
	if !strings.HasSuffix(fileName, ".md") {
		t.Errorf("expected file extension to be .md, got %s", fileName)
	}

	baseName := strings.TrimSuffix(fileName, ".md")
	if len(baseName) != 16 {
		t.Errorf("expected name length to be 16, got %d (name: %s)", len(baseName), baseName)
	}

	for _, r := range baseName {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Errorf("expected lowercase hexadecimal characters in name, got invalid character '%c' in %s", r, baseName)
		}
	}

	// Verify file exists and is empty
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("failed to stat created file: %v", err)
	}

	if info.Size() != 0 {
		t.Errorf("expected created file to be empty, got size %d", info.Size())
	}
}

func TestRunAdd_NonExistentDir(t *testing.T) {
	_, err := RunAdd("this-dir-does-not-exist-hopefully-12345")
	if err == nil {
		t.Errorf("expected error when running on non-existent directory, got nil")
	}
}

func TestRunAdd_NotADirectory(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "regular-file.txt")
	err := os.WriteFile(filePath, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err = RunAdd(filePath)
	if err == nil {
		t.Errorf("expected error when target is a file, not a directory, got nil")
	}
}
