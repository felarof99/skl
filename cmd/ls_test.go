package cmd

import (
	"path/filepath"
	"testing"

	"skl/internal/library"
	"skl/internal/state"
)

func TestSkillLoadedMatchesPrefixedLoadBySourcePath(t *testing.T) {
	src := filepath.Join(t.TempDir(), "thinkhats", "1-blue-open")
	st := &state.State{Version: 1, Loaded: map[string]state.LoadEntry{
		"thinkhats-nithin-1-blue-open": {
			DirName: "thinkhats-nithin-1-blue-open",
			Source:  src,
			Bundles: []string{"thinkhats"},
		},
	}}
	skill := library.Skill{ID: "thinkhats/1-blue-open", SrcPath: src}

	if !skillLoaded(skill, st, loadedSourcePaths(st)) {
		t.Fatalf("prefixed live skill should mark source skill as loaded")
	}
}
