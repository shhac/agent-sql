package dialog

import (
	"fmt"
	"os"
)

// platformAvailable reports whether a GUI dialog can be shown on Windows.
//
// Win32-OpenSSH leaves $SESSIONNAME unset when the SSH server is running
// as a service; service contexts also fail. An interactive desktop
// session has SESSIONNAME as "Console" (physical login) or "RDP-Tcp#N"
// (RDP). We allow anything non-empty.
func platformAvailable() error {
	if os.Getenv("SESSIONNAME") == "" {
		return fmt.Errorf("%w: $SESSIONNAME unset (likely SSH or service context)", ErrNoGUI)
	}
	return nil
}
