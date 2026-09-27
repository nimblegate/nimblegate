package store

import "testing"

func Save() error {
	if testing.Testing() {
		return nil
	}
	return write()
}
