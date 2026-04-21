package blob

import "errors"

// ErrNotFound is returned by Stat and Get when the requested key is absent.
var ErrNotFound = errors.New("blob: not found")
