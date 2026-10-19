//go:build studio_dev && !release

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func (m *devManifest) ensureStopRequest() error {
	path := m.stopPath()
	if err := canonicalDevPath(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		err = json.NewEncoder(f).Encode(devStopRequest{m.sessionID()})
		if err == nil {
			err = f.Sync()
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	} else if !os.IsExist(err) {
		return err
	}
	var request devStopRequest
	if err := readDevJSON(path, &request); err != nil {
		return err
	}
	if request.SessionID != m.sessionID() {
		return fmt.Errorf("stop request belongs to another launch")
	}
	return nil
}
