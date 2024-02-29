package promise

import (
	"errors"
)

var ErrInvalidState = errors.New("invalid promise internal state")
