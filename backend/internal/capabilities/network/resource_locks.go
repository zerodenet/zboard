package network

import "errors"

// ErrResourcesBusy means a current topology read could not acquire an immediate
// lock. The caller's transaction must roll back; retry the same intent after the
// competing mutation completes rather than treating it as an applied result.
var ErrResourcesBusy = errors.New("network resources are being changed; retry the unchanged intent")
