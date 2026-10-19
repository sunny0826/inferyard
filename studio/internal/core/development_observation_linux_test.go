//go:build studio_dev && !release

package core

import (
	"os"
	"syscall"
	"testing"
)

func TestDevelopmentLinuxMissingProcIsNotAbsence(t *testing.T) {
	for _, self := range []string{"", "malformed", "123 (self) Z"} {
		read := func(path string) ([]byte, error) {
			if path == "/proc/self/stat" && self != "" {
				return []byte(self), nil
			}
			return nil, os.ErrNotExist
		}
		state, err := observeLinuxDevelopmentProcess(123, read, func(int) error { return syscall.ESRCH })
		if state != DevelopmentProcessUnknown || err == nil {
			t.Fatal("unusable proc accepted as absence", state, err)
		}
	}
	self := []byte("123 (self) R 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 1")
	for _, nativeErr := range []error{nil, syscall.EPERM, syscall.ESRCH} {
		read := func(path string) ([]byte, error) {
			if path == "/proc/self/stat" {
				return self, nil
			}
			return nil, os.ErrNotExist
		}
		state, err := observeLinuxDevelopmentProcess(123, read, func(int) error { return nativeErr })
		if nativeErr == syscall.ESRCH {
			if state != DevelopmentProcessAbsent || err != nil {
				t.Fatal(state, err)
			}
		} else if state != DevelopmentProcessUnknown || err == nil {
			t.Fatal("native absence not required", state, err)
		}
	}
}
