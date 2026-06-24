// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// EventsFile is the gateway-wide structured event log, JSONL, appended only.
const EventsFile = "_events.jsonl"

// Event records one gateway-wide lifecycle or scan operation.
// Operator is a placeholder ("local") until an auth layer fills it.
type Event struct {
	Timestamp time.Time      `json:"ts"`
	Event     string         `json:"event"`
	Repo      string         `json:"repo"`
	Operator  string         `json:"operator"`
	Payload   map[string]any `json:"payload"`
	OK        bool           `json:"ok"`
}

// AppendEvent writes one event line to <policyRoot>/_events.jsonl, creating the
// file if needed. O_APPEND writes of one JSON-encoded line are atomic on Linux
// for buffers <= PIPE_BUF - events are well under that, so no flock needed.
func AppendEvent(policyRoot string, e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if e.Operator == "" {
		e.Operator = "local"
	}
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	path := filepath.Join(policyRoot, EventsFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(e)
}

// ReadEvents stream-decodes the events file and returns events the keep
// predicate selects. Returns nil + nil if the file does not exist.
func ReadEvents(policyRoot string, keep func(Event) bool) ([]Event, error) {
	path := filepath.Join(policyRoot, EventsFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Event
	dec := json.NewDecoder(bufio.NewReader(f))
	for {
		var e Event
		if err := dec.Decode(&e); err != nil {
			break
		}
		if keep == nil || keep(e) {
			out = append(out, e)
		}
	}
	return out, nil
}
