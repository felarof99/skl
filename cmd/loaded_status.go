package cmd

import (
	"skl/internal/library"
	"skl/internal/state"
)

func loadedSourcePaths(st *state.State) map[string]bool {
	out := map[string]bool{}
	if st == nil {
		return out
	}
	for _, entry := range st.Loaded {
		if entry.Source != "" {
			out[entry.Source] = true
		}
	}
	return out
}

func skillLoaded(skill library.Skill, st *state.State, loadedSources map[string]bool) bool {
	if st != nil {
		if _, ok := st.Loaded[skill.ID]; ok {
			return true
		}
	}
	return loadedSources[skill.SrcPath]
}
