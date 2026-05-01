package dialog

import (
	"errors"
	"testing"
)

func TestPlatformAvailableDarwin(t *testing.T) {
	cases := []struct {
		name          string
		sshConnection string
		termProgram   string
		wantErrIs     error
	}{
		{
			name:          "local terminal app — allowed",
			sshConnection: "",
			termProgram:   "iTerm.app",
			wantErrIs:     nil,
		},
		{
			name:          "no SSH and no TERM_PROGRAM — allowed (let osascript surface its own error)",
			sshConnection: "",
			termProgram:   "",
			wantErrIs:     nil,
		},
		{
			name:          "SSH'd in with a local terminal program — allowed (e.g. user is on Mac with iTerm but happens to also have SSH_CONNECTION set in shared shell)",
			sshConnection: "10.0.0.1 22 10.0.0.2 22",
			termProgram:   "iTerm.app",
			wantErrIs:     nil,
		},
		{
			name:          "SSH'd in, no local terminal — refuse",
			sshConnection: "10.0.0.1 22 10.0.0.2 22",
			termProgram:   "",
			wantErrIs:     ErrNoGUI,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SSH_CONNECTION", tc.sshConnection)
			t.Setenv("TERM_PROGRAM", tc.termProgram)

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
