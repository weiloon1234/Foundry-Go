package logging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// FileID identifies one owned log generation. It is not a filesystem path.
type FileID string

// FileInfo never exposes an absolute server path. IDs expire on owner restart;
// an active ID also expires on clear or rotation.
type FileInfo struct {
	ID       FileID    `json:"id"`
	Name     string    `json:"name"`
	Active   bool      `json:"active"`
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

// FileRead bounds one byte-range read. Offset -1 requests the newest bytes.
// Consumers parse complete lines and use Next to continue; a range can end in
// the middle of a record. Data is owned by the caller and must be treated as text.
type FileRead struct {
	ID     FileID
	Offset int64
	Limit  int
}
type FileChunk struct {
	File         FileInfo
	Data         []byte
	Offset, Next int64
	More         bool
}

const MaxFileReadBytes = 1 << 20

// FileSink borrows an existing rotating file sink. Stacks, streams, borrowed
// loggers and file sinks without rotation are deliberately not file owners.
func (s *ChannelSet) FileSink(name ChannelName) (*Sink, error) {
	if s != nil && s.state != nil {
		for _, leaf := range s.state.leaves {
			if leaf.name == name && leaf.sink != nil && leaf.sink.state.config.Driver == File && !leaf.sink.state.config.Rotation.Disabled {
				return leaf.sink, nil
			}
		}
	}
	return nil, fault.New(fault.Missing, "owned rotating log channel is unavailable")
}

// withFile stops admission under mu and places a barrier behind every accepted
// async record. The writer never takes mu. Cancellation before the barrier does
// not perform the operation; once admitted the native file operation owns its
// result even if the caller's deadline ends.
func (s *Sink) withFile(ctx context.Context, work func(*rotatingFile) error) error {
	if ctx == nil || s == nil || s.state == nil {
		return fault.New(fault.Invalid, "log file control requires an owner and context")
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.closed || state.target == nil {
		return fault.New(fault.Closed, "log sink is not running")
	}
	file, ok := state.file.(*rotatingFile)
	if !ok {
		return fault.New(fault.Missing, "sink has no owned rotating file")
	}
	if state.queue != nil {
		done := make(chan struct{})
		select {
		case state.queue <- asyncRecord{barrier: done}:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return work(file)
}

func (f *rotatingFile) fileID(name string) FileID {
	generation := f.session
	if name == f.name {
		generation += ":" + strconv.FormatUint(f.generation, 10)
	}
	digest := sha256.Sum256([]byte(generation + ":" + name))
	return FileID(hex.EncodeToString(digest[:]))
}
func (f *rotatingFile) fileInfo(name string) (FileInfo, error) {
	info, err := f.root.Lstat(name)
	if err != nil {
		return FileInfo{}, err
	}
	if !info.Mode().IsRegular() {
		return FileInfo{}, fault.New(fault.Invalid, "log file is not regular")
	}
	if name == f.name {
		if f.file == nil {
			return FileInfo{}, fault.New(fault.Conflict, "active log is unavailable")
		}
		opened, err := f.file.Stat()
		if err != nil || !os.SameFile(opened, info) {
			return FileInfo{}, fault.New(fault.Conflict, "active log identity changed")
		}
	}
	return FileInfo{ID: f.fileID(name), Name: name, Active: name == f.name, Bytes: info.Size(), Modified: info.ModTime().UTC()}, nil
}
func (f *rotatingFile) files() ([]FileInfo, error) {
	dir, err := f.root.Open(".")
	if err != nil {
		return nil, err
	}
	archives, readErr := f.readArchives(dir)
	if err := errors.Join(readErr, dir.Close()); err != nil {
		return nil, err
	}
	names := []string{f.name}
	for _, a := range archives {
		names = append(names, a.name)
	}
	result := make([]FileInfo, 0, len(names))
	for _, name := range names {
		info, err := f.fileInfo(name)
		if errors.Is(err, os.ErrNotExist) && name != f.name {
			continue
		} // retention won the race
		if err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	slices.SortFunc(result, func(a, b FileInfo) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		return strings.Compare(b.Name, a.Name)
	})
	return result, nil
}
func (f *rotatingFile) findFile(id FileID) (FileInfo, error) {
	if len(id) != 64 {
		return FileInfo{}, fault.New(fault.Invalid, "invalid log file identity")
	}
	files, err := f.files()
	if err != nil {
		return FileInfo{}, err
	}
	for _, info := range files {
		if info.ID == id {
			return info, nil
		}
	}
	return FileInfo{}, fault.New(fault.Conflict, "log file generation expired")
}
func (s *Sink) Files(ctx context.Context) (files []FileInfo, err error) {
	err = s.withFile(ctx, func(f *rotatingFile) error { var err error; files, err = f.files(); return err })
	return
}
func (s *Sink) ReadFile(ctx context.Context, request FileRead) (chunk FileChunk, err error) {
	if request.Offset < -1 || request.Limit < 1 || request.Limit > MaxFileReadBytes {
		return chunk, fault.New(fault.Invalid, "invalid bounded log read")
	}
	err = s.withFile(ctx, func(f *rotatingFile) error {
		info, err := f.findFile(request.ID)
		if err != nil {
			return err
		}
		file, err := regularLogFile(f.root, info.Name, os.O_RDONLY)
		if err != nil {
			return err
		}
		defer file.Close()
		offset := request.Offset
		if offset == -1 {
			offset = max(0, info.Bytes-int64(request.Limit))
		}
		if offset > info.Bytes {
			return fault.New(fault.Conflict, "log read position expired")
		}
		data := make([]byte, min(int64(request.Limit), info.Bytes-offset))
		n, err := file.ReadAt(data, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		chunk = FileChunk{File: info, Data: data[:n], Offset: offset, Next: offset + int64(n), More: offset+int64(n) < info.Bytes}
		return nil
	})
	return
}

// RotateFile preserves the old file as an archive and installs a fresh active
// descriptor. expected must identify the current active generation.
func (s *Sink) RotateFile(ctx context.Context, expected FileID) error {
	return s.withFile(ctx, func(f *rotatingFile) error {
		info, err := f.findFile(expected)
		if err != nil {
			return err
		}
		if !info.Active {
			return fault.New(fault.Invalid, "rotation requires the active log")
		}
		if err := f.file.Sync(); err != nil {
			return err
		}
		return f.rotate(time.Now())
	})
}

// ClearFile truncates the owned descriptor; it never unlinks the active name.
// Accepted records before the barrier are cleared; later records append normally.
func (s *Sink) ClearFile(ctx context.Context, expected FileID) error {
	return s.withFile(ctx, func(f *rotatingFile) error {
		info, err := f.findFile(expected)
		if err != nil {
			return err
		}
		if !info.Active {
			return fault.New(fault.Invalid, "clear requires the active log")
		}
		if err := f.file.Truncate(0); err != nil {
			return err
		}
		f.size, f.day = 0, calendarDay(time.Now(), f.zone)
		f.generation++
		return f.file.Sync()
	})
}

// DeleteArchive removes only a selected, closed archive of this owner.
func (s *Sink) DeleteArchive(ctx context.Context, id FileID) error {
	return s.withFile(ctx, func(f *rotatingFile) error {
		f.awaitCleanup()
		info, err := f.findFile(id)
		if err != nil {
			return err
		}
		if info.Active {
			return fault.New(fault.Invalid, "cannot delete the active log")
		}
		if err := f.root.Remove(info.Name); err != nil {
			return err
		}
		return nil
	})
}
