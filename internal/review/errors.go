package review

import "errors"

var errIssueClosed = errors.New("issue is closed")

type suppressedFailureCommentError struct {
	cause error
}

func (e *suppressedFailureCommentError) Error() string { return e.cause.Error() }
func (e *suppressedFailureCommentError) Unwrap() error { return e.cause }

// SuppressFailureComment marks an error as one that should be logged but
// should not trigger the handler's generic "Review failed" comment.
func SuppressFailureComment(err error) error {
	if err == nil {
		return nil
	}

	var suppressed *suppressedFailureCommentError
	if errors.As(err, &suppressed) {
		return err
	}

	return &suppressedFailureCommentError{cause: err}
}

// ShouldPostFailureComment reports whether the handler should post the generic
// "Review failed" comment for err.
func ShouldPostFailureComment(err error) bool {
	var suppressed *suppressedFailureCommentError
	return !errors.As(err, &suppressed)
}
