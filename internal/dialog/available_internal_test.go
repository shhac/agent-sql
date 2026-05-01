package dialog

import (
	"errors"
	"testing"
)

// TestPlatformAvailableShape asserts that platformAvailable returns
// either nil (GUI looks usable) or an error wrapping a sentinel. It does
// not assert which result, since this depends on the test machine's
// real environment.
func TestPlatformAvailableShape(t *testing.T) {
	err := platformAvailable()
	if err == nil {
		return
	}
	if !errors.Is(err, ErrNoGUI) && !errors.Is(err, ErrUnsupported) {
		t.Fatalf("platformAvailable() returned unexpected error shape: %v", err)
	}
}
