package generate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

const journalName = "journal.json"
const stagingProtocol = "protocol-v1"

type fileState struct {
	Exists bool        `json:"exists"`
	Hash   string      `json:"hash,omitempty"`
	Mode   os.FileMode `json:"mode,omitempty"`
}
type journalEntry struct {
	Before fileState `json:"before"`
	After  fileState `json:"after"`
}
type journal struct {
	Version      int                          `json:"version"`
	Files        map[string]journalEntry      `json:"files"`
	Guards       []string                     `json:"guards,omitempty"`
	FieldNotes   []string                     `json:"field_notes,omitempty"`
	Distribution *pluginmanifest.Distribution `json:"distribution,omitempty"`
	NewDirs      []string                     `json:"new_dirs,omitempty"`
}

func state(file oldFile) fileState {
	if !file.exists {
		return fileState{}
	}
	return fileState{true, hash(file.data), file.mode}
}
func (s fileState) matches(file oldFile) bool { return s == state(file) }

// Recovery reports whether a interrupted publication was kept or rolled back.
type Recovery struct{ Found, Kept, RolledBack bool }

// Recover acquires the package's process guard, validates every journal target,
// and keeps a fully published set or restores an interrupted set. It never
// replaces a target that differs from both recorded states.
func Recover(ctx context.Context, dir string) (Recovery, error) {
	if !automaticRecovery {
		return Recovery{}, fmt.Errorf("automatic recovery is not supported on this OS; inspect retained staging manually")
	}
	if err := ctx.Err(); err != nil {
		return Recovery{}, err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return Recovery{}, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return Recovery{}, err
	}
	if err := rejectAncestorPublication(absolute); err != nil {
		return Recovery{}, err
	}
	if _, err := os.Lstat(filepath.Join(absolute, lockName)); errors.Is(err, os.ErrNotExist) {
		return Recovery{}, nil
	} else if err != nil {
		return Recovery{}, err
	}
	before, err := readWithin(absolute, filepath.Join(lockName, journalName))
	if err != nil {
		return Recovery{}, err
	}
	var dirs []string
	if before.exists {
		log, err := decodeJournal(before.data)
		if err != nil {
			return Recovery{}, err
		}
		dirs = log.Guards
	}
	release, err := acquireGuards(absolute, dirs)
	if err != nil {
		return Recovery{}, err
	}
	defer release()
	current, err := readWithin(absolute, filepath.Join(lockName, journalName))
	if err != nil {
		return Recovery{}, err
	}
	if state(current) != state(before) {
		return Recovery{}, fmt.Errorf("generation journal changed while acquiring recovery guards; retry")
	}
	return recoverLocked(ctx, absolute)
}

func recoverLocked(ctx context.Context, dir string) (Recovery, error) {
	lock := filepath.Join(dir, lockName)
	info, err := os.Lstat(lock)
	if errors.Is(err, os.ErrNotExist) {
		return Recovery{}, nil
	}
	if err != nil {
		return Recovery{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Recovery{}, fmt.Errorf("generation staging must be a regular directory")
	}
	result := Recovery{Found: true}
	contents, err := readWithin(dir, filepath.Join(lockName, journalName))
	if err != nil {
		return result, err
	}
	if !contents.exists {
		// Versioned staging never changes targets before publishing the journal.
		// Do not apply this rule to retained files from older generator versions.
		marker, err := readFile(filepath.Join(lock, stagingProtocol))
		if err != nil {
			return result, err
		}
		entries, err := os.ReadDir(lock)
		if err != nil {
			return result, err
		}
		if (!marker.exists && len(entries) != 0) || (marker.exists && string(marker.data) != "1\n") {
			return result, fmt.Errorf("unrecognized generation staging; inspect it manually")
		}
		return result, discardStaging(dir)
	}
	log, err := decodeJournal(contents.data)
	if err != nil {
		return result, err
	}
	if err := validateGuardDirs(dir, log.Guards); err != nil {
		return result, err
	}
	if err := validateCreatedDistributionDirectories(dir, log.NewDirs); err != nil {
		return result, err
	}
	allAfter := true
	current := make(map[string]oldFile, len(log.Files))
	currentBytes := 0
	for _, name := range sortedNames(log.Files) {
		entry := log.Files[name]
		file, err := readWithin(dir, name)
		if err != nil {
			return result, err
		}
		currentBytes += len(file.data)
		if log.Version == 6 && currentBytes > 2*MaxDistributionBytes+MaxDistributionFileBytes {
			return result, fmt.Errorf("plugin recovery targets exceed their byte bound")
		}
		if !entry.Before.matches(file) && !entry.After.matches(file) {
			return result, fmt.Errorf("recovery refuses changed target %s; journal and backups retained", name)
		}
		current[name] = file
		allAfter = allAfter && entry.After.matches(file)
	}
	if err := validateRecoveryFieldDocumentation(dir, log, current); err != nil {
		return result, err
	}
	if err := validateRecoveryDistribution(dir, log); err != nil {
		return result, err
	}
	if allAfter {
		if log.Version == 6 {
			if err := syncDistributionDirectories(dir, log.NewDirs); err != nil {
				return result, err
			}
			if err := syncGuards(dir, log.Guards); err != nil {
				return result, err
			}
		}
		result.Kept = true
		return result, discardStaging(dir)
	}
	// Check all backups before the first restoration. A failed or canceled
	// recovery keeps the immutable journal/backups and can safely be retried.
	backups := make(map[string]oldFile)
	backupBytes := 0
	for _, name := range sortedNames(log.Files) {
		entry := log.Files[name]
		if !entry.Before.Exists || entry.Before.matches(current[name]) {
			continue
		}
		backup, err := readWithin(dir, filepath.Join(lockName, stageName("old", name)))
		if err != nil {
			return result, err
		}
		if !entry.Before.matches(backup) {
			return result, fmt.Errorf("invalid recovery backup for %s; staging retained", name)
		}
		backupBytes += len(backup.data)
		if log.Version == 6 && backupBytes > MaxDistributionBytes+MaxDistributionFileBytes {
			return result, fmt.Errorf("plugin recovery backups exceed their byte bound")
		}
		backups[name] = backup
	}
	rename, close, err := confinedRename(dir)
	if err != nil {
		return result, err
	}
	defer close()
	for _, name := range publicationOrder(log.Files) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entry := log.Files[name]
		if entry.Before.matches(current[name]) {
			continue
		}
		// Repeat the target check immediately before mutation, including mode.
		file, err := readWithin(dir, name)
		if err != nil {
			return result, err
		}
		if state(file) != state(current[name]) {
			return result, fmt.Errorf("target %s changed during recovery", name)
		}
		if err := restoreFile(dir, name, backups[name], rename); err != nil {
			return result, err
		}
	}
	if err := syncGuards(dir, log.Guards); err != nil {
		return result, err
	}
	if err := syncDistributionDirectories(dir, log.NewDirs); err != nil {
		return result, err
	}
	result.RolledBack = true
	return result, discardStaging(dir)
}

func decodeJournal(data []byte) (journal, error) {
	var log journal
	if err := decodePublicationJSON(data, &log); err != nil {
		return log, fmt.Errorf("invalid generation journal: %w", err)
	}
	if (log.Version != 1 && log.Version != 2 && log.Version != 3 && log.Version != 4 && log.Version != 5 && log.Version != 6 && log.Version != 7) || len(log.Files) == 0 {
		return log, fmt.Errorf("unsupported generation journal")
	}
	if (log.Version == 6) != (log.Distribution != nil) || (log.Version != 6 && len(log.NewDirs) != 0) {
		return log, fmt.Errorf("plugin distribution requires a version 6 journal")
	}
	if log.Version == 6 {
		if err := log.Distribution.Validate(); err != nil {
			return log, err
		}
		if len(log.Files) > 2*MaxDistributionFiles+1 || len(log.Guards)+len(log.NewDirs) > MaxDistributionDirectories+1 {
			return log, fmt.Errorf("plugin journal exceeds its resource bounds")
		}
		if entry, ok := log.Files[manifestName]; !ok || !entry.After.Exists {
			return log, fmt.Errorf("plugin journal requires its ownership manifest")
		}
	}
	notes := make(map[string]bool)
	if (log.Version == 4) != (len(log.FieldNotes) > 0) {
		return log, fmt.Errorf("field documentation requires a version 4 journal")
	}
	for _, name := range log.FieldNotes {
		entry, exists := log.Files[name]
		if !exists || notes[name] || !fieldDocumentationName(filepath.Base(name)) ||
			!entry.Before.Exists || !entry.After.Exists || entry.Before.Mode != entry.After.Mode {
			return log, fmt.Errorf("invalid field documentation journal entry")
		}
		notes[name] = true
	}
	dirs := make(map[string]bool)
	if log.Version == 1 {
		if len(log.Guards) != 0 {
			return log, fmt.Errorf("legacy journal cannot contain graph guards")
		}
		dirs["."] = true
	} else {
		for _, dir := range log.Guards {
			if !localPath(dir) || dirs[dir] {
				return log, fmt.Errorf("invalid journal guard directory")
			}
			if log.Version == 6 && dir != "." && !DistributionPath(dir) {
				return log, fmt.Errorf("invalid plugin journal guard")
			}
			dirs[dir] = true
		}
		if !dirs["."] {
			return log, fmt.Errorf("graph journal requires its root guard")
		}
	}
	if log.Version == 6 {
		for _, name := range log.NewDirs {
			if !DistributionPath(name) || dirs[name] || !dirs[filepath.ToSlash(filepath.Dir(name))] {
				return log, fmt.Errorf("invalid plugin journal creation directory")
			}
			dirs[name] = true
		}
	}
	for name, entry := range log.Files {
		base := filepath.Base(name)
		validTarget := base == manifestName || outputName.MatchString(base) || notes[name]
		if log.Version == 5 {
			validTarget = len(log.Guards) == 1 && log.Guards[0] == "." && name == base && (base == manifestName || artifactName.MatchString(base))
		}
		if log.Version == 6 {
			validTarget = name == manifestName || DistributionPath(name)
		}
		if log.Version == 3 || log.Version == 7 {
			// Both versions permit only one new consumer-owned file. Version 3
			// retains its original migration/seeder authority; version 7 adds
			// the other scaffold kinds without replacement or deletion rights.
			allowed := legacyScaffoldName.MatchString(base)
			if log.Version == 7 {
				allowed = scaffoldName.MatchString(base)
			}
			validTarget = len(log.Files) == 1 && len(log.Guards) == 1 && log.Guards[0] == "." && name == base && allowed && !entry.Before.Exists && entry.After.Exists
		}
		if !localPath(name) || !dirs[filepath.ToSlash(filepath.Dir(name))] || !validTarget {
			return log, fmt.Errorf("unsafe journal target")
		}
		for _, s := range []fileState{entry.Before, entry.After} {
			if s.Mode&^os.ModePerm != 0 || (!s.Exists && (s.Hash != "" || s.Mode != 0)) {
				return log, fmt.Errorf("invalid journal file state")
			}
			if s.Exists && !validDigest(s.Hash) {
				return log, fmt.Errorf("invalid journal digest")
			}
		}
	}
	return log, nil
}

func prepareStaging(dir string, plan writePlan) error {
	lock := filepath.Join(dir, lockName)
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("create generation staging: %w; run foundry generate --recover", err)
	}
	if err := writeSynced(filepath.Join(lock, stagingProtocol), []byte("1\n"), 0600); err != nil {
		return err
	}
	log := journal{Version: 1, Files: make(map[string]journalEntry)}
	if len(plan.guards) > 0 {
		log.Version = 2
		log.Guards = plan.guards
	}
	if plan.scaffold {
		log.Version = 7
	}
	if len(plan.fieldNotes) > 0 {
		log.Version = 4
		log.FieldNotes = sortedNames(plan.fieldNotes)
	}
	if plan.artifacts {
		log.Version = 5
	}
	if plan.distribution != nil {
		log.Version = 6
		log.Distribution = plan.distribution
		log.NewDirs = plan.newDirs
		data, exists := plan.changes[manifestName]
		if !exists || data == nil {
			return fmt.Errorf("plugin distribution publication requires an ownership commit")
		}
		mode := os.FileMode(0644)
		if previous := plan.before[manifestName]; previous.exists {
			mode = previous.mode
		}
		if err := writeSynced(filepath.Join(lock, distributionOwnershipProof), data, mode); err != nil {
			return err
		}
	}
	for _, name := range sortedNames(plan.changes) {
		old := plan.before[name]
		entry := journalEntry{Before: state(old)}
		if data := plan.changes[name]; data != nil {
			mode := os.FileMode(0644)
			if old.exists {
				mode = old.mode
			}
			if err := writeSynced(filepath.Join(lock, stageName("new", name)), data, mode); err != nil {
				return err
			}
			if plan.fieldNotes[name] {
				if err := writeSynced(filepath.Join(lock, fieldDocumentationProof(name)), data, mode); err != nil {
					return err
				}
			}
			entry.After = state(oldFile{true, data, mode})
		}
		if old.exists {
			if err := writeSynced(filepath.Join(lock, stageName("old", name)), old.data, old.mode); err != nil {
				return err
			}
		}
		log.Files[name] = entry
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(lock, "journal.tmp"), append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(lock, "journal.tmp"), filepath.Join(lock, journalName)); err != nil {
		return err
	}
	if err := syncDir(lock); err != nil {
		return err
	}
	return syncDir(dir)
}

func restoreFile(dir, name string, old oldFile, rename func(string, string) error) error {
	if !old.exists {
		err := removeWithin(dir, name)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// Copy the backup so a second crash never consumes the only original.
	file, err := os.CreateTemp(filepath.Join(dir, lockName), "restore-")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	if err := finishSynced(file, old.data, old.mode); err != nil {
		return err
	}
	return rename(path, filepath.Join(dir, name))
}

func writeSynced(path string, data []byte, mode os.FileMode) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return finishSynced(file, data, mode)
}

func finishSynced(file *os.File, data []byte, mode os.FileMode) (err error) {
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	return file.Sync()
}
func syncDir(dir string) (err error) {
	if !automaticRecovery {
		return nil
	}
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	return file.Sync()
}

// Move finished staging away atomically before deleting backups. A crash during
// cleanup cannot leave an apparently recoverable journal with missing backups.
func discardStaging(dir string) error {
	finished, err := os.MkdirTemp(dir, ".foundry-generate.finished-")
	if err != nil {
		return err
	}
	// Some mounted filesystems reject renaming onto even an empty directory.
	// Reserve a unique name, then free it before moving completed staging there.
	if err := os.Remove(finished); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(dir, lockName), finished); err != nil {
		_ = os.Remove(finished)
		return err
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	if err := os.RemoveAll(finished); err != nil {
		return fmt.Errorf("remove finished generation staging %s: %w", finished, err)
	}
	return syncDir(dir)
}

func publicationOrder[V any](files map[string]V) []string {
	names := sortedNames(files)
	ordered := make([]string, 0, len(names))
	for _, name := range names {
		if filepath.Base(name) != manifestName {
			ordered = append(ordered, name)
		}
	}
	for _, name := range names {
		if filepath.Base(name) == manifestName {
			ordered = append(ordered, name)
		}
	}
	return ordered
}
