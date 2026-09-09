//go:build !windows

package core

import (
	"os"
	"syscall"
)

func lockProcessFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
