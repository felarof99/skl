package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"skl/internal/library"
	"skl/internal/state"
)

type LoadAction struct {
	Skill   library.Skill
	Already bool
}

type LoadPlan struct {
	Bundle  string
	Actions []LoadAction
}

type UnloadAction struct {
	SkillID string
	Entry   state.LoadEntry
	Remove  bool
}

type UnloadPlan struct {
	Bundle  string
	Actions []UnloadAction
}

func PlanLoad(bundleName string, skills []string, lib []library.Skill, st *state.State) (LoadPlan, error) {
	return PlanLoadEntries(bundleName, library.BundleEntriesForSkills(skills), lib, st)
}

func PlanLoadEntries(bundleName string, entries []library.BundleEntry, lib []library.Skill, st *state.State) (LoadPlan, error) {
	plan := LoadPlan{Bundle: bundleName}
	index := indexLibrary(lib)

	for _, entry := range entries {
		expanded, err := expandBundleEntry(bundleName, entry, index)
		if err != nil {
			return plan, err
		}
		for _, skill := range expanded {
			_, loaded := st.Loaded[skill.ID]
			plan.Actions = append(plan.Actions, LoadAction{Skill: skill, Already: loaded})
		}
	}
	return plan, nil
}

func expandBundleEntry(bundleName string, entry library.BundleEntry, index map[string]library.Skill) ([]library.Skill, error) {
	switch {
	case entry.Skill != "":
		s, ok := index[entry.Skill]
		if !ok {
			return nil, fmt.Errorf("bundle %q references unknown skill %q", bundleName, entry.Skill)
		}
		prefix := bundleName
		if entry.Prefix != "" {
			prefix = entry.Prefix
		}
		return expandRepoSkill(prefix, s)
	case entry.Folder != "":
		prefix := entry.Prefix
		if prefix == "" {
			prefix = bundleName
		}
		return expandFolderEntry(bundleName, entry, prefix)
	default:
		return nil, fmt.Errorf("bundle %q contains empty entry", bundleName)
	}
}

func PlanUnload(bundleName string, st *state.State) UnloadPlan {
	plan := UnloadPlan{Bundle: bundleName}
	for id, entry := range st.Loaded {
		owns := false
		for _, b := range entry.Bundles {
			if b == bundleName {
				owns = true
				break
			}
		}
		if !owns {
			continue
		}
		remove := len(entry.Bundles) == 1
		plan.Actions = append(plan.Actions, UnloadAction{
			SkillID: id,
			Entry:   entry,
			Remove:  remove,
		})
	}
	return plan
}

func indexLibrary(lib []library.Skill) map[string]library.Skill {
	out := make(map[string]library.Skill, len(lib))
	for _, s := range lib {
		out[s.ID] = s
	}
	return out
}

type manifestDir struct {
	dir  string
	name string
	rel  string
}

func expandRepoSkill(bundleName string, skill library.Skill) ([]library.Skill, error) {
	manifests, err := skillManifestDirs(skill.SrcPath)
	if err != nil {
		return nil, err
	}
	if len(manifests) <= 1 {
		return []library.Skill{skill}, nil
	}

	sourceName := skill.ID
	for _, manifest := range manifests {
		if manifest.rel == "." {
			sourceName = manifest.name
			break
		}
	}

	var out []library.Skill
	seen := map[string]bool{}
	for _, manifest := range manifests {
		name := prefixedName(bundleName, sourceName, manifest)
		if seen[name] {
			return nil, fmt.Errorf("bundle %q maps multiple skills to %q", bundleName, name)
		}
		seen[name] = true
		out = append(out, library.Skill{
			ID:           name,
			DirName:      name,
			SrcPath:      manifest.dir,
			External:     skill.External,
			Repo:         skill.Repo,
			NameOverride: name,
		})
	}
	return out, nil
}

func expandFolderEntry(bundleName string, entry library.BundleEntry, prefix string) ([]library.Skill, error) {
	skillsRoot, err := library.SkillsPath()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(skillsRoot, filepath.FromSlash(entry.Folder))
	var manifests []manifestDir
	if len(entry.Skills) > 0 {
		for _, skill := range entry.Skills {
			manifest, err := manifestForFolderSkill(root, skill)
			if err != nil {
				return nil, fmt.Errorf("bundle %q folder %q references %q: %w", bundleName, entry.Folder, skill, err)
			}
			manifests = append(manifests, manifest)
		}
	} else {
		manifests, err = skillManifestDirs(root)
		if err != nil {
			return nil, err
		}
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("bundle %q folder %q contains no skills", bundleName, entry.Folder)
	}

	sourceName := entry.Folder
	for _, manifest := range manifests {
		if manifest.rel == "." {
			sourceName = manifest.name
			break
		}
	}

	var out []library.Skill
	seen := map[string]bool{}
	for _, manifest := range manifests {
		name := prefixedName(prefix, sourceName, manifest)
		if seen[name] {
			return nil, fmt.Errorf("bundle %q maps multiple skills to %q", bundleName, name)
		}
		seen[name] = true
		out = append(out, library.Skill{
			ID:           name,
			DirName:      name,
			SrcPath:      manifest.dir,
			NameOverride: name,
		})
	}
	return out, nil
}

func manifestForFolderSkill(root, skill string) (manifestDir, error) {
	rel := strings.Trim(strings.TrimSpace(skill), "/")
	if rel == "" || rel == "." {
		rel = "."
	}
	dir := root
	if rel != "." {
		dir = filepath.Join(root, filepath.FromSlash(rel))
	}
	path := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(path); err != nil {
		return manifestDir{}, err
	}
	name := readManifestName(path)
	if name == "" {
		name = filepath.Base(dir)
	}
	return manifestDir{dir: dir, name: name, rel: filepath.ToSlash(rel)}, nil
}

func skillManifestDirs(root string) ([]manifestDir, error) {
	var manifests []manifestDir
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "SKILL.md" {
			return nil
		}
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return err
		}
		name := readManifestName(path)
		if name == "" {
			name = filepath.Base(dir)
		}
		manifests = append(manifests, manifestDir{dir: dir, name: name, rel: rel})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(manifests, func(i, j int) bool {
		if manifests[i].rel == "." {
			return true
		}
		if manifests[j].rel == "." {
			return false
		}
		return manifests[i].rel < manifests[j].rel
	})
	return manifests, nil
}

func readManifestName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return ""
		}
		if strings.HasPrefix(trimmed, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		}
	}
	return ""
}

func prefixedName(bundleName, sourceID string, manifest manifestDir) string {
	if manifest.rel == "." {
		return bundleName
	}
	suffix := manifest.name
	if strings.HasPrefix(suffix, sourceID+"-") {
		suffix = strings.TrimPrefix(suffix, sourceID+"-")
	} else if strings.HasPrefix(suffix, sourceID) {
		suffix = strings.TrimPrefix(suffix, sourceID)
		suffix = strings.TrimPrefix(suffix, "-")
	}
	return bundleName + "-" + suffix
}
