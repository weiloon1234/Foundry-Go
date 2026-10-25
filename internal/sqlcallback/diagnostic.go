package sqlcallback

import "github.com/weiloon1234/Foundry-Go/internal/callback"

// Retain restores a source-issued diagnostic hidden by a native SQL wrapper,
// after execution and outcome classification, preserving every returned cause.
func Retain(foreign, result error) error { return callback.RetainDiagnostic(foreign, result) }
