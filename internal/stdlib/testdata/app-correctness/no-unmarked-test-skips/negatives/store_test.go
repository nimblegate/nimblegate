package store

import "testing"

func TestSave(t *testing.T) {
	if err := Save(); err != nil {
		t.Fatal(err)
	}
}
