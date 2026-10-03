//go:build !foundry_vips || !cgo

package imaging

import "testing"

func TestNativeBackendUnavailableIsExplicit(t *testing.T) {
	config := DefaultConfig()
	config.Backend = LibvipsBackend
	if engine, err := New(config); err == nil || engine != nil {
		t.Fatal("native selection silently fell back")
	}
}
