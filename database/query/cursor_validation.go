package query

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

// CursorInputError identifies malformed or foreign cursor input without exposing values.
type CursorInputError = querycore.CursorInputError

func cursorInputFailure(cause error) error { return querycore.CursorInputFailure(cause) }
