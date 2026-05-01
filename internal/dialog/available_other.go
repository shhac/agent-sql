//go:build !darwin && !linux && !windows

package dialog

import (
	"fmt"
	"runtime"
)

func platformAvailable() error {
	return fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}
