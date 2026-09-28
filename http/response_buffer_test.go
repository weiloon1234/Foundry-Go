package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"
)

func TestResponseBufferPreservesCaptureAcrossPagesAndOverflow(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), responseBufferPageBytes/4)
	buffer := responseBuffer{limit: int64(len(payload))}
	// Exercise non-aligned appends; readers and hashes must see the same bytes.
	for offset := 0; offset < len(payload); {
		end := min(offset+777, len(payload))
		if !buffer.append(payload[offset:end]) {
			t.Fatal("bounded payload rejected")
		}
		offset = end
	}
	if buffer.append([]byte("overflow")) {
		t.Fatal("oversized response accepted")
	}
	var capacity int64
	for _, page := range buffer.pages {
		capacity += int64(cap(page))
	}
	if capacity > buffer.limit {
		t.Fatal("page capacity exceeded configured byte ceiling", capacity)
	}
	actual, err := io.ReadAll(buffer.reader())
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("capture changed after overflow", err)
	}
	hash := sha256.Sum256(payload)
	if string(buffer.entityTag()) != "\""+hex.EncodeToString(hash[:])+"\"" {
		t.Fatal("validator does not describe captured bytes")
	}
	for _, offset := range []int64{0, 1, responseBufferPageBytes - 1, responseBufferPageBytes, int64(len(payload)) - 1, int64(len(payload)), int64(len(payload)) + 1} {
		got, want := make([]byte, 129), make([]byte, 129)
		n, err := buffer.ReadAt(got, offset)
		expectedN, expectedErr := bytes.NewReader(payload).ReadAt(want, offset)
		if n != expectedN || !errors.Is(err, expectedErr) || !bytes.Equal(got, want) {
			t.Fatal("ReadAt diverged", offset, n, err, expectedN, expectedErr)
		}
	}
	if _, err := buffer.ReadAt(make([]byte, 1), -1); !errors.Is(err, os.ErrInvalid) {
		t.Fatal("negative read offset", err)
	}
	reader := buffer.reader()
	if _, err := reader.Seek(-13, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	tail, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(tail, payload[len(payload)-13:]) {
		t.Fatal("conditional range reader diverged", err)
	}
	buffer.reset()
	if buffer.size != 0 || buffer.pages != nil {
		t.Fatal("capture retained pages after release")
	}
}

func TestResponseBufferEmptyPartialPageAndRejectedAppend(t *testing.T) {
	for _, limit := range []int64{1, responseBufferPageBytes - 1, responseBufferPageBytes + 1} {
		buffer := responseBuffer{limit: limit}
		payload := bytes.Repeat([]byte("x"), int(limit))
		if buffer.append(append(bytes.Clone(payload), 'y')) || buffer.size != 0 {
			t.Fatal("overflow partially consumed input")
		}
		if !buffer.append(payload) {
			t.Fatal("exact ceiling rejected")
		}
		got, err := io.ReadAll(buffer.reader())
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatal("partial page changed payload", err)
		}
	}
	empty := responseBuffer{limit: 1}
	hash := sha256.Sum256(nil)
	if string(empty.entityTag()) != "\""+hex.EncodeToString(hash[:])+"\"" {
		t.Fatal("empty response hash")
	}
	if err := empty.entityTag().Validate(); err != nil {
		t.Fatal(err)
	}
}
