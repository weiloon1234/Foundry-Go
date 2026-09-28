package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type SinkDriver string

const (
	Stderr SinkDriver = "stderr"
	Stdout SinkDriver = "stdout"
	File   SinkDriver = "file"
)

// SinkConfig selects an application-owned JSON destination. File sinks append
// with automatic rotation/retention unless Rotation.Disabled is explicit.
type SinkConfig struct {
	Driver    SinkDriver
	Path      string
	Level     slog.Level
	AddSource bool
	Rotation  RotationConfig
	// TimeZone controls timestamps and daily rotation; empty means UTC for direct
	// sinks and inherits the application timezone during configured assembly.
	TimeZone temporal.ZoneName
}

func DefaultSinkConfig() SinkConfig { return SinkConfig{Driver: Stderr} }
func (c SinkConfig) Validate() error {
	if _, err := c.location(); err != nil {
		return err
	}
	if c.Level < slog.LevelDebug || c.Level > slog.LevelError {
		return fault.New(fault.Invalid, "invalid logging level")
	}
	switch c.Driver {
	case Stderr, Stdout:
		if c.Path != "" || c.Rotation != (RotationConfig{}) {
			return fault.New(fault.Invalid, "stream logging cannot select file options")
		}
	case File:
		if !filepath.IsAbs(c.Path) || strings.ContainsRune(c.Path, 0) {
			return fault.New(fault.Invalid, "log file requires an absolute path")
		}
		return c.Rotation.Validate()
	default:
		return fault.New(fault.Invalid, "unsupported logging sink")
	}
	return nil
}

func (c SinkConfig) location() (*time.Location, error) {
	zone := c.TimeZone
	if zone == "" {
		zone = temporal.UTC
	}
	return zone.Location()
}

// Sink is a prepared writer; it acquires its destination only in Start. Copying
// the handle retains one owner. It never closes process stdout/stderr.
type Sink struct{ state *sinkState }
type sinkState struct {
	mu       sync.Mutex
	config   SinkConfig
	writer   io.Writer
	file     io.Closer
	closed   bool
	location *time.Location
}

func PrepareSink(c SinkConfig) (*Sink, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	location, err := c.location()
	if err != nil {
		return nil, err
	}
	return &Sink{state: &sinkState{config: c, location: location}}, nil
}
func (s *Sink) Logger() *slog.Logger {
	if s == nil || s.state == nil {
		return nil
	}
	return JSON(s, Options{Level: s.state.config.Level, AddSource: s.state.config.AddSource, Location: s.state.location})
}
func (s *Sink) Start(ctx context.Context) error {
	if s == nil || s.state == nil || ctx == nil {
		return fault.New(fault.Invalid, "logging sink requires an owner and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return fault.New(fault.Closed, "logging sink is closed")
	}
	if state.writer != nil {
		return nil
	}
	switch state.config.Driver {
	case Stderr:
		state.writer = os.Stderr
	case Stdout:
		state.writer = os.Stdout
	case File:
		if !state.config.Rotation.Disabled {
			file, err := openRotatingFile(state.config.Path, state.config.Rotation, time.Now(), state.location)
			if err != nil {
				return err
			}
			state.writer, state.file = file, file
			return nil
		}
		if info, err := os.Lstat(state.config.Path); err == nil {
			if !info.Mode().IsRegular() {
				return fault.New(fault.Invalid, "log destination must be a regular file")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fault.Wrap(fault.Invalid, "cannot inspect log destination", err)
		}
		file, err := os.OpenFile(state.config.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fault.Wrap(fault.Invalid, "cannot open log destination", err)
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return errors.Join(fault.New(fault.Invalid, "invalid opened log destination"), err, file.Close())
		}
		state.writer = file
		state.file = file
	}
	return nil
}
func (s *Sink) Write(data []byte) (int, error) {
	if s == nil || s.state == nil {
		return 0, io.ErrClosedPipe
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed || state.writer == nil {
		return 0, io.ErrClosedPipe
	}
	return state.writer.Write(data)
}
func (s *Sink) Close() error {
	if s == nil || s.state == nil {
		return nil
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return nil
	}
	state.closed = true
	state.writer = nil
	if state.file != nil {
		if err := state.file.Close(); err != nil {
			return fault.Wrap(fault.Internal, "cannot close log destination", err)
		}
	}
	return nil
}
