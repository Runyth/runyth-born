//go:build !linux && !darwin && !windows

package console

import "os"

func enableRawMode(_ *os.File) (func() error, bool) {
	return nil, false
}
