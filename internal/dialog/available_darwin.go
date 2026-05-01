package dialog

import (
	"fmt"
	"os"
)

// platformAvailable reports whether a GUI dialog can be shown on macOS.
//
// osascript fails cleanly if no Aqua session is attached, so we let the
// dialog itself surface most failures. The one case we pre-flight is
// "obviously SSH'd in": $SSH_CONNECTION is set and no local terminal app
// has set $TERM_PROGRAM. That combination almost never has a usable GUI
// session for the SSH'd user.
func platformAvailable() error {
	if os.Getenv("SSH_CONNECTION") != "" && os.Getenv("TERM_PROGRAM") == "" {
		return fmt.Errorf("%w: appears to be an SSH session with no local terminal", ErrNoGUI)
	}
	return nil
}
