package upload

import (
	"os"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const captureDirectoryPrefix = "foundry-upload-"

// Only the exclusive serving owner may reconcile a dead owner's captures.
// Unknown entries refuse startup instead of being treated as free capacity.
func reconcileCaptures(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return safeerror.WrapFailure(fault.Invalid, "upload pool reconciliation could not open directory", err)
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return safeerror.WrapFailure(fault.Invalid, "upload pool reconciliation could not read directory", err)
	}
	for _, entry := range entries {
		if entry.Name() == ownerMarker {
			continue
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), captureDirectoryPrefix) {
			return safeerror.NewFault(fault.Invalid, "upload pool reconciliation found an unknown entry; reconcile the private directory before serving")
		}
		if err := root.RemoveAll(entry.Name()); err != nil {
			return safeerror.WrapFailure(fault.Internal, "upload pool reconciliation could not remove a retained capture; serving refused", err)
		}
	}
	return nil
}
