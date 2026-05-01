package dialog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformAvailableLinux(t *testing.T) {
	// All cases need a PATH with neither zenity nor kdialog unless the
	// case explicitly puts one there. Set an empty PATH per case via
	// t.Setenv (each case gets its own PATH).
	emptyDir := t.TempDir()
	withZenity := makeFakeBin(t, "zenity")
	withKdialog := makeFakeBin(t, "kdialog")

	cases := []struct {
		name      string
		display   string
		wayland   string
		path      string
		wantErrIs error
	}{
		{
			name:    "no display server — refuse",
			display: "", wayland: "",
			path:      emptyDir,
			wantErrIs: ErrNoGUI,
		},
		{
			name:    "X11 display, no zenity/kdialog — refuse",
			display: ":0", wayland: "",
			path:      emptyDir,
			wantErrIs: ErrNoGUI,
		},
		{
			name:    "Wayland display, no zenity/kdialog — refuse",
			display: "", wayland: "wayland-0",
			path:      emptyDir,
			wantErrIs: ErrNoGUI,
		},
		{
			name:    "X11 display + zenity on PATH — allowed",
			display: ":0", wayland: "",
			path:      withZenity,
			wantErrIs: nil,
		},
		{
			name:    "Wayland display + kdialog on PATH — allowed",
			display: "", wayland: "wayland-0",
			path:      withKdialog,
			wantErrIs: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DISPLAY", tc.display)
			t.Setenv("WAYLAND_DISPLAY", tc.wayland)
			t.Setenv("PATH", tc.path)

			err := platformAvailable()
			if tc.wantErrIs == nil {
				if err != nil {
					t.Errorf("platformAvailable() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErrIs) {
				t.Errorf("platformAvailable() = %v, want errors.Is(%v) = true", err, tc.wantErrIs)
			}
		})
	}
}

// makeFakeBin returns a directory containing a single executable file
// with the given name, suitable for prefixing $PATH so exec.LookPath
// finds it. The file is just `#!/bin/sh\nexit 0` — never invoked.
func makeFakeBin(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}
