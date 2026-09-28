package email_test

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type trackingStorage struct {
	storage.Backend
	opened, closed atomic.Int32
	failRead       bool
}

func (s *trackingStorage) Open(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	body, info, err := s.Backend.Open(ctx, key, options)
	if body != nil {
		s.opened.Add(1)
		body = &trackingReader{ReadCloser: body, storage: s}
	}
	return body, info, err
}

type trackingReader struct {
	io.ReadCloser
	storage *trackingStorage
}

func (r *trackingReader) Read(p []byte) (int, error) {
	if r.storage.failRead {
		return 0, io.ErrUnexpectedEOF
	}
	return r.ReadCloser.Read(p)
}
func (r *trackingReader) Close() error { r.storage.closed.Add(1); return r.ReadCloser.Close() }
func TestAttachmentReadersCloseBeforeSubmissionAndOnReadFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "resolved", true: "read-failed"}[fail], func(t *testing.T) {
			var tracked *trackingStorage
			registry, attachment := attachmentStore(t, func(peer storage.Backend) storage.Backend {
				tracked = &trackingStorage{Backend: peer, failRead: fail}
				return tracked
			})
			var sent atomic.Int32
			m := mailer(t, email.DriverFunc(func(context.Context, email.Outbound) (email.Receipt, error) {
				sent.Add(1)
				if tracked.closed.Load() != 1 {
					t.Error("attachment reader retained during transport")
				}
				return email.Receipt{}, nil
			}), registry, nil)
			_, err := m.Send(t.Context(), message(t).Attach(attachment), email.SendOptions{})
			if fail {
				if !errors.Is(err, email.Transient) || sent.Load() != 0 {
					t.Fatal("storage failure reached transport", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tracked.opened.Load() != 1 || tracked.closed.Load() != 1 {
				t.Fatal("attachment reader ownership leaked", tracked.opened.Load(), tracked.closed.Load())
			}
		})
	}
}
