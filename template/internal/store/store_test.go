package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type StoreSuite struct {
	suite.Suite
	dir string
}

func TestStoreSuite(t *testing.T) {
	suite.Run(t, new(StoreSuite))
}

func (s *StoreSuite) SetupTest() {
	s.dir = s.T().TempDir()
}

func (s *StoreSuite) TestOpen() {
	cases := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"existing dir", filepath.Join(s.T().TempDir(), "state.json"), ""},
		{"missing dir", filepath.Join(s.T().TempDir(), "none", "state.json"), "opening the store"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			st, err := Open(tc.path)
			if tc.wantErr != "" {
				s.Require().ErrorContains(err, tc.wantErr)
				s.Require().Nil(st)
				return
			}
			s.Require().NoError(err)
			s.Require().NotNil(st)
		})
	}
}

func (s *StoreSuite) TestAddAndLoad() {
	st, err := Open(filepath.Join(s.dir, "state.json"))
	s.Require().NoError(err)

	got, err := st.Load()
	s.Require().NoError(err)
	s.Require().Equal(State{}, got)

	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	_, err = st.Add(Count{Glob: "*.go", Files: 3, At: at})
	s.Require().NoError(err)
	got, err = st.Add(Count{Files: 5, Truncated: true, At: at})
	s.Require().NoError(err)
	want := State{Last: &Count{Files: 5, Truncated: true, At: at}, Runs: 2}
	s.Require().Equal(want, got)

	reopened, err := Open(filepath.Join(s.dir, "state.json"))
	s.Require().NoError(err)
	got, err = reopened.Load()
	s.Require().NoError(err)
	s.Require().Equal(want, got)

	entries, err := os.ReadDir(s.dir)
	s.Require().NoError(err)
	s.Require().Len(entries, 1, "no temporary files are left behind")
}

func (s *StoreSuite) TestErrors() {
	cases := []struct {
		name    string
		setup   func(path string)
		wantErr string
	}{
		{"corrupt file", func(p string) { s.Require().NoError(os.WriteFile(p, []byte("{"), 0o600)) }, "decoding the state"},
		{"unreadable file", func(p string) { s.Require().NoError(os.Mkdir(p, 0o700)) }, "reading the state"},
		{"read-only dir", func(p string) { s.Require().NoError(os.Chmod(filepath.Dir(p), 0o500)) }, "writing the state"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			dir := s.T().TempDir()
			s.T().Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			path := filepath.Join(dir, "state.json")
			st, err := Open(path)
			s.Require().NoError(err)
			tc.setup(path)
			_, err = st.Add(Count{Files: 1})
			s.Require().ErrorContains(err, tc.wantErr)
		})
	}
}
