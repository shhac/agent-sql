package dialog

import (
	"errors"
	"testing"
)

func TestPlatformAvailableWindows(t *testing.T) {
	cases := []struct {
		name        string
		sessionName string
		wantErrIs   error
	}{
		{
			name:        "physical console session — allowed",
			sessionName: "Console",
			wantErrIs:   nil,
		},
		{
			name:        "RDP session — allowed",
			sessionName: "RDP-Tcp#0",
			wantErrIs:   nil,
		},
		{
			name:        "SESSIONNAME unset (Win32-OpenSSH or service context) — refuse",
			sessionName: "",
			wantErrIs:   ErrNoGUI,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SESSIONNAME", tc.sessionName)

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
