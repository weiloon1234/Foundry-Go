// Package encryption provides bounded, versioned AES-256-GCM envelopes using
// Go's random-nonce AEAD. Keys are explicit application configuration; the
// package never generates a replacement for a missing decryption key. Bind each
// value to its purpose and owning record through Context. It does not provide
// streaming encryption, key custody or a distributed key-usage counter.
package encryption
