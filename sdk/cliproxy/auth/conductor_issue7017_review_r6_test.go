package auth

import ()

// Review round 6 regression: a Home aggregate immediate (explicit zero from
// an earlier credential) must not be replaced by the last attempt's positive
// trusted hint, in headers or scheduling.
