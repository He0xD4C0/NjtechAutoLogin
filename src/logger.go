package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const logRetentionDays = 25

type Logger struct {
	mu        sync.Mutex
	out       io.Writer
	file      *os.File
	date      string
	logger    *log.Logger
	logDir    string
	cleanupCh chan struct{}
	closeOnce sync.Once
}

func NewFileLogger(logDir string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, err
	}
	l := &Logger{logDir: logDir, cleanupCh: make(chan struct{})}
	if err := l.rotateLocked(); err != nil {
		return nil, err
	}
	l.cleanup()
	go l.cleanupLoop()
	return l, nil
}

func NewStreamLogger(out io.Writer) *Logger {
	return &Logger{out: out, logger: log.New(out, "", log.LstdFlags)}
}

func (l *Logger) rotateLocked() error {
	if l.file == nil && l.logDir == "" {
		return nil
	}
	newDate := time.Now().Format("2006-01-02")
	if l.date == newDate && l.file != nil {
		return nil
	}
	filename := filepath.Join(l.logDir, fmt.Sprintf("njtechlogin-%s.log", newDate))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	old := l.file
	l.file = f
	l.out = f
	l.date = newDate
	l.logger = log.New(f, "", log.LstdFlags)
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (l *Logger) Printf(format string, values ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.rotateLocked(); err != nil {
		log.Printf("[njtechlogin] 日志轮转失败: %v", err)
		return
	}
	l.logger.Printf(format, values...)
}

func (l *Logger) Println(values ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.rotateLocked(); err != nil {
		log.Printf("[njtechlogin] 日志轮转失败: %v", err)
		return
	}
	l.logger.Println(values...)
}

func (l *Logger) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.cleanup()
		case <-l.cleanupCh:
			return
		}
	}
}

func (l *Logger) cleanup() {
	files, err := filepath.Glob(filepath.Join(l.logDir, "njtechlogin-*.log"))
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -logRetentionDays)
	for _, path := range files {
		info, err := os.Stat(path)
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
		}
	}
}

func (l *Logger) Close() error {
	var err error
	l.closeOnce.Do(func() {
		if l.cleanupCh != nil {
			close(l.cleanupCh)
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.file != nil {
			err = l.file.Close()
		}
	})
	return err
}
