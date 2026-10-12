// Package store keeps the plugin's state in one JSON file under the data
// dir. A plugin with more to keep can use SQLite instead (modernc.org/sqlite
// needs no CGO).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Count is one count of the channel's files.
type Count struct {
	Glob      string    `json:"glob"`
	Files     int       `json:"files"`
	Truncated bool      `json:"truncated,omitempty"`
	At        time.Time `json:"at"`
}

// State is what the store keeps: the last count and how many there were.
type State struct {
	Last *Count `json:"last,omitempty"`
	Runs int    `json:"runs"`
}

// Store reads and writes the state file.
type Store struct {
	path string
	mu   sync.Mutex
}

// Open returns a Store keeping its state at path, whose dir must exist.
func Open(path string) (*Store, error) {
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("opening the store: %w", err)
	}
	return &Store{path: path}, nil
}

// Load returns the state, empty before the first Add.
func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Add records c as the last count.
func (s *Store) Add(c Count) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return State{}, err
	}
	st.Last = &c
	st.Runs++
	return st, s.save(st)
}

func (s *Store) load() (State, error) {
	var st State
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("reading the state: %w", err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("decoding the state: %w", err)
	}
	return st, nil
}

// save writes st to a temporary file and renames it over the state, so a
// crash never leaves half a file.
func (s *Store) save(st State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("encoding the state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*")
	if err != nil {
		return fmt.Errorf("writing the state: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing the state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing the state: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("writing the state: %w", err)
	}
	return nil
}
