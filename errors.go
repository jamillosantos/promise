package promise

import (
	"errors"
)

// ErrInvalidState is returned by Await for a promise that has no settle
// channel and is still pending. It cannot arise from promises built with the
// public API (New, Resolved, Rejected); it surfaces misuse such as awaiting a
// zero-value Promise.
var ErrInvalidState = errors.New("invalid promise internal state")

// ErrExitedWithoutResult is the rejection error used when the promise
// function exits without returning a result: it called runtime.Goexit, or it
// panicked with nil under the legacy GODEBUG=panicnil=1 semantics where
// recover reports no panic.
var ErrExitedWithoutResult = errors.New("promise function exited without returning")
