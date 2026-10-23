package querycore

import "github.com/weiloon1234/Foundry-Go/fault"

type CursorInputError struct{ cause error }

func (*CursorInputError) Error() string { return "invalid cursor input or query scope" }

func (e *CursorInputError) GoString() string { return e.Error() }

func (e *CursorInputError) Unwrap() error { return e.cause }

func (*CursorInputError) Is(target error) bool { return target == fault.Invalid }

func CursorInputFailure(cause error) error { return &CursorInputError{cause: cause} }
