package review

// This file defines error-wrapping patterns for controlling failure comment
// behavior. When the review engine encounters certain failures (closed issues,
// comparison limits), it should log the error but NOT post a generic "Review
// failed" comment to the issue — either because a more specific comment was
// already posted, or because commenting would be noise.

import "errors"

// errIssueClosed is returned when the linked issue is closed. Reviews for
// closed issues are skipped without posting any comment.
var errIssueClosed = errors.New("issue is closed")

// suppressedFailureCommentError wraps errors that should NOT trigger the
// handler's generic "Review failed" comment. The wrapped error is still
// logged. Use SuppressFailureComment to wrap, ShouldPostFailureComment to check.
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
