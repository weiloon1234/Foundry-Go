package logging

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
)

const archiveTimeFormat = "20060102T150405.000000000Z"
const maxLogDirectoryEntries = 10000

// pruneInterval separates ordinary write-triggered retention cleanups.
const pruneInterval = time.Hour

// rotatingFile writes are serialized by its owning sink. Retention cleanup runs
// in one owned worker, off the record write path; os.Root is safe for that
// concurrent directory access. The stable lock is held across renames and
// released only after the active descriptor closes and the worker exits.
// Never unlink the lock.
type rotatingFile struct {
	root   *os.Root
	lock   *os.File
	file   *os.File
	name   string
	policy RotationConfig
	size   int64
	day    int
	zone   *time.Location
	events *sinkEvents
	// Rotation retry state is owned by the serialized writer.
	rotateAfter    time.Time
	rotateFailures int
	// nextPrune and retryAfter (Unix nanoseconds) are shared with the cleanup
	// worker. Writes request cleanup at or after nextPrune; rollover requests it
	// immediately unless a failed cleanup is still inside its retry backoff.
	nextPrune     atomic.Int64
	retryAfter    atomic.Int64
	pruneFailures int
	cleanup       chan time.Time
	barrier       chan chan struct{}
	stop          chan struct{}
	stopped       chan struct{}
}

func openRotatingFile(path string, policy RotationConfig, now time.Time, zone *time.Location, events *sinkEvents) (_ *rotatingFile, result error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "cannot open log directory", err)
	}
	ownedZone := *zone
	f := &rotatingFile{root: root, name: filepath.Base(path), policy: policy.resolved(), zone: &ownedZone, events: events}
	defer func() {
		if result != nil {
			result = errors.Join(result, f.Close())
		}
	}()
	f.lock, err = regularLogFile(root, f.name+".foundry-rotation.lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	info, err := f.lock.Stat()
	if err != nil || info.Size() != 0 {
		return nil, fault.New(fault.Invalid, "invalid log rotation lock")
	}
	locked, err := filelock.Try(f.lock)
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "cannot lock log destination", err)
	}
	if !locked {
		return nil, fault.New(fault.Conflict, "log destination already has a rotation owner")
	}
	f.file, err = regularLogFile(root, f.name, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return nil, err
	}
	info, err = f.file.Stat()
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "cannot inspect active log", err)
	}
	f.size, f.day = info.Size(), calendarDay(info.ModTime(), f.zone)
	// Startup cleanup is synchronous but never prevents logging; a failure is
	// counted and retried with backoff by the worker.
	f.pruneCompleted(now, f.prune(now))
	f.cleanup, f.barrier = make(chan time.Time, 1), make(chan chan struct{})
	f.stop, f.stopped = make(chan struct{}), make(chan struct{})
	go f.cleanupLoop()
	return f, nil
}

// The parent directory is operator-owned. Root confines symlink resolution;
// Lstat plus identity checks reject existing links and destination substitutions.
func regularLogFile(root *os.Root, name string, flags int) (*os.File, error) {
	before, err := root.Lstat(name)
	if err == nil && !before.Mode().IsRegular() {
		return nil, fault.New(fault.Invalid, "log destination must be a regular file")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fault.Wrap(fault.Invalid, "cannot inspect log destination", err)
	}
	file, err := root.OpenFile(name, flags, 0600)
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "cannot open log destination", err)
	}
	opened, statErr := file.Stat()
	after, pathErr := root.Lstat(name)
	if statErr != nil || pathErr != nil || !opened.Mode().IsRegular() || !after.Mode().IsRegular() || !os.SameFile(opened, after) || (before != nil && !os.SameFile(before, opened)) {
		return nil, errors.Join(fault.New(fault.Invalid, "log destination changed while opening"), statErr, pathErr, file.Close())
	}
	return file, nil
}

// Compare calendar labels, not fixed 24-hour buckets: DST days may be 23/25
// hours and some historical timezone changes skip a whole local date.
func calendarDay(now time.Time, zone *time.Location) int {
	year, month, day := now.In(zone).Date()
	return year*10000 + int(month)*100 + day
}

func (f *rotatingFile) Write(data []byte) (int, error) { return f.writeAt(data, time.Now()) }

// writeAt never drops a record because of rotation or retention. A failed
// rollover keeps appending to the current descriptor and retries after a
// bounded backoff; only an absent active file that cannot be reopened fails.
func (f *rotatingFile) writeAt(data []byte, now time.Time) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if int64(len(data)) > f.policy.MaxBytes {
		return 0, fault.New(fault.Invalid, "log record exceeds rotation size limit")
	}
	// A failed replacement leaves the archived data intact. Retry only opening the
	// absent active path; never append into an archive or truncate a replacement.
	if f.file == nil {
		if err := f.openActive(now); err != nil {
			return 0, err
		}
	}
	if f.size > 0 && (int64(len(data)) > f.policy.MaxBytes-f.size || calendarDay(now, f.zone) > f.day) && !now.Before(f.rotateAfter) {
		if err := f.rotate(now); err != nil {
			f.rotateFailures++
			f.rotateAfter = now.Add(retryDelay(f.rotateFailures))
			f.events.rotationFailed(err)
			if f.file == nil {
				if err := f.openActive(now); err != nil {
					return 0, err
				}
			}
		} else {
			f.rotateFailures, f.rotateAfter = 0, time.Time{}
		}
	}
	if now.UnixNano() >= f.nextPrune.Load() {
		f.requestCleanup(now)
	}
	if f.size == 0 {
		f.day = calendarDay(now, f.zone)
	}
	n, err := f.file.Write(data)
	f.size += int64(n)
	return n, err
}

// requestCleanup signals the worker without blocking a record write. The one
// pending slot keeps the latest time: an older queued request would prune with
// a stale time and leave newly expired archives until the next prune interval.
// A later time removes everything an earlier one would. The serialized writer
// is the only sender, so once the slot is emptied the next send succeeds.
func (f *rotatingFile) requestCleanup(now time.Time) {
	f.nextPrune.Store(now.Add(pruneInterval).UnixNano())
	for {
		select {
		case f.cleanup <- now:
			return
		default:
		}
		select {
		case queued := <-f.cleanup:
			if queued.After(now) {
				now = queued
			}
		default:
		}
	}
}

func (f *rotatingFile) cleanupLoop() {
	defer close(f.stopped)
	for {
		select {
		case <-f.stop:
			return
		case done := <-f.barrier:
			// Complete a request queued before the barrier; select alone
			// does not order simultaneously ready cases.
			select {
			case now := <-f.cleanup:
				f.pruneCompleted(now, f.prune(now))
			default:
			}
			close(done)
		case now := <-f.cleanup:
			f.pruneCompleted(now, f.prune(now))
		}
	}
}

// pruneCompleted runs on the worker (or before it starts). A failure is counted
// and rescheduled with backoff instead of failing a record write.
func (f *rotatingFile) pruneCompleted(now time.Time, err error) {
	if err == nil {
		f.pruneFailures = 0
		f.retryAfter.Store(0)
		f.nextPrune.Store(now.Add(pruneInterval).UnixNano())
		return
	}
	f.pruneFailures++
	retry := now.Add(retryDelay(f.pruneFailures)).UnixNano()
	f.retryAfter.Store(retry)
	f.nextPrune.Store(retry)
	f.events.pruneFailed(err)
}

// awaitCleanup waits until every cleanup requested before the call completed.
func (f *rotatingFile) awaitCleanup() {
	done := make(chan struct{})
	select {
	case f.barrier <- done:
		<-done
	case <-f.stopped:
	}
}

func (f *rotatingFile) openActive(now time.Time) error {
	file, err := regularLogFile(f.root, f.name, os.O_CREATE|os.O_EXCL|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}
	f.file, f.size, f.day = file, 0, calendarDay(now, f.zone)
	return nil
}

func (f *rotatingFile) rotate(now time.Time) error {
	current, statErr := f.file.Stat()
	active, pathErr := f.root.Lstat(f.name)
	if statErr != nil || pathErr != nil || !active.Mode().IsRegular() || !os.SameFile(current, active) {
		return errors.Join(fault.New(fault.Invalid, "active log destination changed before rotation"), statErr, pathErr)
	}
	// Reserve an unpredictable archive name exclusively before rename. No prior
	// archive is overwritten, including when clocks repeat or move backwards.
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fault.Wrap(fault.Internal, "cannot name log archive", err)
	}
	name := f.name + ".foundry-" + now.UTC().Format(archiveTimeFormat) + "-" + hex.EncodeToString(suffix[:]) + ".log"
	reserved, err := f.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fault.Wrap(fault.Internal, "cannot reserve log archive", err)
	}
	if err := reserved.Close(); err != nil {
		return errors.Join(err, f.root.Remove(name))
	}
	if err := f.root.Rename(f.name, name); err != nil {
		return errors.Join(fault.Wrap(fault.Internal, "cannot rotate log file", err), f.root.Remove(name))
	}
	closeErr := f.file.Close()
	f.file = nil
	if err := errors.Join(closeErr, f.openActive(now)); err != nil {
		return fault.Wrap(fault.Internal, "cannot replace active log", err)
	}
	if now.UnixNano() >= f.retryAfter.Load() {
		f.requestCleanup(now)
	}
	return nil
}

type logArchive struct {
	name string
	time time.Time
}

func (f *rotatingFile) archiveTime(name string) (time.Time, bool) {
	value, ok := strings.CutPrefix(name, f.name+".foundry-")
	if !ok || len(value) != len(archiveTimeFormat)+1+32+len(".log") || !strings.HasSuffix(value, ".log") || value[len(archiveTimeFormat)] != '-' {
		return time.Time{}, false
	}
	if _, err := hex.DecodeString(value[len(archiveTimeFormat)+1 : len(value)-4]); err != nil {
		return time.Time{}, false
	}
	timestamp, err := time.Parse(archiveTimeFormat, value[:len(archiveTimeFormat)])
	return timestamp, err == nil && timestamp.Format(archiveTimeFormat) == value[:len(archiveTimeFormat)]
}

func (f *rotatingFile) prune(now time.Time) error {
	dir, err := f.root.Open(".")
	if err != nil {
		return fault.Wrap(fault.Internal, "cannot inspect log archives", err)
	}
	archives, readErr := f.readArchives(dir)
	if err := errors.Join(readErr, dir.Close()); err != nil {
		return fault.Wrap(fault.Internal, "cannot inspect log archives", err)
	}
	slices.SortFunc(archives, func(a, b logArchive) int { return strings.Compare(b.name, a.name) })
	for index, archive := range archives {
		if index < f.policy.MaxFiles && archive.time.After(now.Add(-f.policy.MaxAge)) {
			continue
		}
		if err := f.root.Remove(archive.name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fault.Wrap(fault.Internal, "cannot remove expired log archive", err)
		}
	}
	return nil
}

func (f *rotatingFile) readArchives(dir *os.File) ([]logArchive, error) {
	var archives []logArchive
	count := 0
	for {
		entries, err := dir.ReadDir(128)
		count += len(entries)
		if count > maxLogDirectoryEntries {
			return nil, fault.New(fault.Invalid, "log directory exceeds entry limit")
		}
		for _, entry := range entries {
			timestamp, match := f.archiveTime(entry.Name())
			if match && entry.Type().IsRegular() {
				archives = append(archives, logArchive{entry.Name(), timestamp})
			}
		}
		if errors.Is(err, io.EOF) {
			return archives, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (f *rotatingFile) Close() error {
	if f.stop != nil {
		close(f.stop)
		<-f.stopped
		f.stop = nil
	}
	var result error
	if f.file != nil {
		result = f.file.Close()
		f.file = nil
	}
	if f.lock != nil {
		result = errors.Join(result, f.lock.Close())
		f.lock = nil
	}
	if f.root != nil {
		result = errors.Join(result, f.root.Close())
		f.root = nil
	}
	return result
}
