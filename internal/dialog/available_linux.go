package dialog

import (
	"fmt"
	"os"
	"os/exec"
)

// platformAvailable reports whether a GUI dialog can be shown on Linux.
// Requires a display server ($DISPLAY or $WAYLAND_DISPLAY) and either
// `zenity` (GNOME / generic) or `kdialog` (KDE) on PATH.
func platformAvailable() error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return fmt.Errorf("%w: no $DISPLAY or $WAYLAND_DISPLAY set", ErrNoGUI)
	}
	if _, err := exec.LookPath("zenity"); err == nil {
		return nil
	}
	if _, err := exec.LookPath("kdialog"); err == nil {
		return nil
	}
	return fmt.Errorf("%w: install `zenity` (GNOME) or `kdialog` (KDE)", ErrNoGUI)
}
