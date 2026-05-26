package logger

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

type Logger struct {
	stdout   *slog.Logger
	stderr   *slog.Logger
	file     *slog.Logger
	fileOnly bool
}

type Config struct {
	FilePath string
	FileOnly bool
	Debug    bool
}

func New(cfg Config) (*Logger, error) {
	l := &Logger{
		fileOnly: cfg.FileOnly,
		stdout: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})),
		stderr: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelError,
		})),
	}

	if cfg.FilePath != "" {
		absPath, err := filepath.Abs(cfg.FilePath)
		if err != nil {
			return nil, fmt.Errorf("resolve log path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
		f, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		l.file = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}

	return l, nil
}

func (l *Logger) Debug(msg string, args ...any) {
	if !l.fileOnly {
		l.stdout.Debug(msg, args...)
	}
	if l.file != nil {
		l.file.Debug(msg, args...)
	}
}

func (l *Logger) Info(msg string, args ...any) {
	if !l.fileOnly {
		l.stdout.Info(msg, args...)
	}
	if l.file != nil {
		l.file.Info(msg, args...)
	}
}

func (l *Logger) Warn(msg string, args ...any) {
	if !l.fileOnly {
		l.stdout.Warn(msg, args...)
	}
	if l.file != nil {
		l.file.Warn(msg, args...)
	}
}

func (l *Logger) Error(msg string, args ...any) {
	if !l.fileOnly {
		l.stderr.Error(msg, args...)
	}
	if l.file != nil {
		l.file.Error(msg, args...)
	}
}

func (l *Logger) Fatal(msg string, args ...any) {
	l.Error(msg, args...)
	os.Exit(1)
}
