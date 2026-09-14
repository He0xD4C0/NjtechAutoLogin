package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLoggerWritesPrivatelyAndCleansOldLogs(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "njtechlogin-2000-01-01.log")
	if err := os.WriteFile(oldPath, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().AddDate(0, 0, -(logRetentionDays + 1))
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	logger, err := NewFileLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger.Println("hello")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old log was not removed: %v", err)
	}
	currentPath := filepath.Join(dir, "njtechlogin-"+time.Now().Format("2006-01-02")+".log")
	info, err := os.Stat(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("log mode = %o", info.Mode().Perm())
	}
}
