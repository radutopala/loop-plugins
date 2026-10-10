package quiz

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/loopapi"
)

// FocusItem is a file the agent is told to read, and why.
type FocusItem struct {
	Path   string
	Reason string
}

var sourceExts = map[string]bool{
	".go": true, ".py": true, ".rb": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".ts": true, ".tsx": true, ".mts": true, ".vue": true, ".svelte": true, ".java": true, ".kt": true,
	".kts": true, ".scala": true, ".rs": true, ".c": true, ".h": true, ".cc": true, ".cpp": true,
	".hpp": true, ".cs": true, ".swift": true, ".m": true, ".php": true, ".ex": true, ".exs": true,
	".erl": true, ".hs": true, ".lua": true, ".dart": true, ".zig": true, ".clj": true, ".sh": true,
	".sql": true, ".ml": true, ".fs": true, ".jl": true, ".r": true, ".pl": true,
}

var skipDirs = map[string]bool{
	"vendor": true, "node_modules": true, "third_party": true, "thirdparty": true, "external": true,
	"dist": true, "build": true, "out": true, "target": true, "bin": true, "obj": true,
	"generated": true, "gen": true, "testdata": true, "fixtures": true, "__pycache__": true,
	"coverage": true, "site-packages": true, "Pods": true, "migrations": true,
}

var generatedMarkers = []string{
	".min.", ".pb.", "_pb2", ".gen.", "_gen.", "_generated.", ".generated.", "_string.go",
	"_test.", ".test.", ".spec.", "_mock.", "mock_", ".d.ts", "zz_",
}

// IsSource reports whether p looks like hand-written, non-test source code:
// a known source extension, outside vendored, generated or hidden dirs, and
// not a lockfile, test, mock or generated file.
func IsSource(p string) bool {
	if !sourceExts[strings.ToLower(path.Ext(p))] {
		return false
	}
	parts := strings.Split(p, "/")
	for _, dir := range parts[:len(parts)-1] {
		if skipDirs[dir] || strings.HasPrefix(dir, ".") {
			return false
		}
	}
	base := strings.ToLower(parts[len(parts)-1])
	if strings.HasPrefix(base, "test_") || strings.HasPrefix(base, ".") {
		return false
	}
	for _, m := range generatedMarkers {
		if strings.Contains(base, m) {
			return false
		}
	}
	return true
}

// Area is the part of the code a file belongs to: its dir.
func Area(p string) string {
	return path.Dir(p)
}

// plan picks the files the agent reads: due areas first, then the most
// churned files, then complexity hotspots, then random source files.
// dirPath is the channel's dir, stripped from absolute hotspot paths.
func (s *Service) plan(ctx context.Context, dirPath, today string, size int) ([]FocusItem, error) {
	tree, err := s.Loop.Tree(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("listing the channel's files: %w", err)
	}
	var sources []string
	for _, f := range tree {
		if IsSource(f) {
			sources = append(sources, f)
		}
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("the channel has no source files to ask about")
	}
	inTree := map[string]bool{}
	byArea := map[string][]string{}
	for _, f := range sources {
		inTree[f] = true
		byArea[Area(f)] = append(byArea[Area(f)], f)
	}

	var out []FocusItem
	seen := map[string]bool{}
	add := func(p, reason string) bool {
		if len(out) >= size || seen[p] || !inTree[p] {
			return false
		}
		seen[p] = true
		out = append(out, FocusItem{Path: p, Reason: reason})
		return true
	}

	due, err := s.Store.DueAreas(ctx, today, max(1, size/3))
	if err != nil {
		return nil, err
	}
	for _, a := range due {
		if files := byArea[a]; len(files) > 0 {
			add(files[s.intn(len(files))], fmt.Sprintf("its area %s is due for review", a))
		}
	}

	if recent, err := s.Loop.GitRecent(ctx, 14); err != nil {
		s.Logger.Info("skipping recent changes", "err", err)
	} else {
		files := slices.Clone(recent.Files)
		slices.SortStableFunc(files, func(a, b loopapi.FileChurn) int { return b.Commits - a.Commits })
		budget := (size + 1) / 2
		for _, f := range files {
			if budget == 0 {
				break
			}
			if add(f.Path, fmt.Sprintf("changed in %d recent commits", f.Commits)) {
				budget--
			}
		}
	}

	if hot, err := s.Loop.Hotspots(ctx, 20); err != nil {
		s.Logger.Info("skipping hotspots", "err", err)
	} else {
		budget := 2
		for _, h := range hot {
			if budget == 0 {
				break
			}
			p := h.Path
			if dirPath != "" {
				p = strings.TrimPrefix(p, strings.TrimSuffix(dirPath, "/")+"/")
			}
			if add(p, fmt.Sprintf("complex function %s at line %d", h.Name, h.StartLine)) {
				budget--
			}
		}
	}

	rest := slices.Clone(sources)
	s.shuffle(rest)
	for _, p := range rest {
		if len(out) >= size {
			break
		}
		add(p, "picked at random")
	}
	return out, nil
}
