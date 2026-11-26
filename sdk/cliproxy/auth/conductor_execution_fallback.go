package auth

import "errors"

func isSessionAffinityRetryError(err error) bool {
	if err == nil {
		return false
	}
	var authErr *Error
	return errors.As(err, &authErr) && authErr != nil && authErr.Code == "session_auth_retry"
}

func shouldReturnLastErrorOnPickFailure(lastErr error, errPick error) bool {
	return lastErr != nil && !isRequestStopError(errPick) && !isSessionAffinityRetryError(errPick)
}
