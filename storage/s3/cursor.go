package s3

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func decodeCursor[T any](cursor storage.Cursor, result *T) error {
	if len(cursor.Token()) > storage.MaxCursorBytes {
		return safeerror.Owned(string(storage.Invalid), storage.Invalid)
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor.Token())
	if err != nil {
		return safeerror.Owned(string(storage.Invalid), storage.Invalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return safeerror.Owned(string(storage.Invalid), storage.Invalid)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return safeerror.Owned(string(storage.Invalid), storage.Invalid)
	}
	return nil
}
func encodeCursor[T any](value T) (storage.Cursor, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return storage.Cursor{}, err
	}
	text := base64.RawURLEncoding.EncodeToString(data)
	if len(text) > storage.MaxCursorBytes {
		return storage.Cursor{}, safeerror.Owned(string(storage.LimitExceeded), storage.LimitExceeded)
	}
	return storage.NewCursor(text), nil
}
