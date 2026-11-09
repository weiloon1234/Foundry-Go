package work

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var validID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

type Payload struct {
	ID      string `json:"id"`
	Barrier bool   `json:"barrier"`
}

type Receipts struct{ root string }

func NewReceipts(root string) *Receipts { return &Receipts{root: root} }

func ValidateID(id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("fixture job requires a canonical UUIDv4")
	}
	return nil
}

func (r *Receipts) Mark(name string) error {
	if err := os.MkdirAll(r.root, 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(r.root, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		_, err = r.Released(name)
		return err
	}
	if err != nil {
		return err
	}
	return file.Close()
}

func (r *Receipts) Released(name string) (bool, error) {
	info, err := os.Lstat(filepath.Join(r.root, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("fixture release marker must be a regular file")
	}
	return true, nil
}

func (r *Receipts) Handle(ctx context.Context, payload Payload) error {
	if err := ValidateID(payload.ID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.Mark(payload.ID + ".started"); err != nil {
		return err
	}
	if payload.Barrier {
		poll := time.NewTicker(20 * time.Millisecond)
		defer poll.Stop()
		for {
			released, err := r.Released(payload.ID + ".release")
			if err != nil {
				return err
			}
			if released {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-poll.C:
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Mark(payload.ID + ".completed")
}
