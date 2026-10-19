//go:build studio_dev && !release

package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Use the permanent owner lock without leaving a blocked waiter after cancellation.
func DevelopmentLockEngineOwner(ctx context.Context, workspace string) (func(), error) {
	check := func(path string, directory bool) error {
		resolved, err := Resolve(path, path)
		if err != nil || resolved != path {
			return fmt.Errorf("noncanonical owner path: %s", path)
		}
		st, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if directory && !st.IsDir() || !directory && !st.Mode().IsRegular() {
			return fmt.Errorf("invalid owner path: %s", path)
		}
		return nil
	}
	if err := check(workspace, true); err != nil {
		return nil, err
	}
	path := ownerPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := check(filepath.Dir(path), true); err != nil {
		return nil, err
	}
	lock := path + ".lock"
	if _, err := os.Lstat(lock); err == nil {
		if err := check(lock, false); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			f.Close()
		}
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		locked, err := tryDevelopmentOwnerLock(f)
		if err != nil {
			return nil, err
		}
		if locked {
			success = true
			return func() { unlockOwnerFile(f); f.Close() }, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
