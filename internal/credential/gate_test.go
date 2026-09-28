package credential

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLateCancellationRetainsOperationalCauseWithoutDisclosure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cause := errors.New("private backend failure")
	err := NewGate(1, time.Second).Execute(ctx, func(context.Context) error { cancel(); return cause })
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatal("cancellation erased operation cause", err)
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("private cause disclosed")
	}
}
