package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	ID           string
	DirName      string
	SrcPath      string
	External     bool
	Repo         string
	NameOverride string
}

type BundleEntry struct {
	Skill  string   `yaml:"skill,omitempty"`
	Folder string   `yaml:"folder,omitempty"`
	Prefix string   `yaml:"prefix,omitempty"`
	Skills []string `yaml:"skills,omitempty"`
}

const ReservedInboxBundle = "inbox"

type bundleFile struct {
	Bundles map[string][]BundleEntry `yaml:"bundles"`
}

func (e *BundleEntry) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		e.Skill = strings.TrimSpace(value.Value)
		return nil
	case yaml.MappingNode:
		type rawEntry BundleEntry
		var raw rawEntry
		if err := value.Decode(&raw); err != nil {
			return err
		}
		if raw.Skill != "" && raw.Folder != "" {
			return fmt.Errorf("bundle entry cannot set both skill and folder")
		}
		if raw.Skill == "" && raw.Folder == "" && raw.Prefix == "" {
			return fmt.Errorf("bundle entry must set skill, folder, or prefix")
		}
		if raw.Skill == "" && raw.Folder == "" && len(raw.Skills) > 0 {
			return fmt.Errorf("bundle entry cannot set skills without folder")
		}
		*e = BundleEntry(raw)
		return nil
	default:
		return fmt.Errorf("unsupported bundle entry")
	}
}

func (e BundleEntry) MarshalYAML() (any, error) {
	if e.Skill != "" && e.Folder == "" && e.Prefix == "" && len(e.Skills) == 0 {
		return e.Skill, nil
	}
	type rawEntry BundleEntry
	return rawEntry(e), nil
}

func LibraryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "skl", "library"), nil
}

func SkillsPath() (string, error) {
	root, err := LibraryPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "skills"), nil
}

func ExternalPath() (string, error) {
	root, err := LibraryPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "external"), nil
}

func BundlesPath() (string, error) {
	root, err := LibraryPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bundles.yaml"), nil
}

func EnsureLibrary() error {
	skills, err := SkillsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(skills, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", skills, err)
	}
	return nil
}

// Skills walks the single skills/ directory. A subdir with a SKILL.md is a loose
// skill (ID = its name). A subdir without one is a "pack" whose children are the
// skills (ID = pack/skill). The legacy external/ directory, if still present, is
// scanned the same way for back-compat.
func Skills() ([]Skill, error) {
	if err := EnsureLibrary(); err != nil {
		return nil, err
	}
	skillsRoot, _ := SkillsPath()
	externalRoot, _ := ExternalPath()
	out := append(scanRoot(skillsRoot), scanRoot(externalRoot)...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// scanRoot returns every skill directly under root (loose skills) and one level
// down inside pack subdirs. A missing root yields no skills.
func scanRoot(root string) []Skill {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if hasSkillManifest(dir) {
			out = append(out, Skill{ID: e.Name(), DirName: e.Name(), SrcPath: dir})
			continue
		}
		// no manifest at this level → treat the dir as a pack of skills
		out = append(out, scanPack(dir, e.Name())...)
	}
	return out
}

// scanPack returns the skills inside a pack subdir, namespaced as pack/skill.
func scanPack(packDir, packName string) []Skill {
	children, err := os.ReadDir(packDir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, c := range children {
		if !c.IsDir() || strings.HasPrefix(c.Name(), ".") {
			continue
		}
		dir := filepath.Join(packDir, c.Name())
		if !hasSkillManifest(dir) {
			continue
		}
		out = append(out, Skill{
			ID:       packName + "/" + c.Name(),
			DirName:  c.Name(),
			SrcPath:  dir,
			External: true,
			Repo:     packName,
		})
	}
	return out
}

func FindSkill(id string) (*Skill, error) {
	all, err := Skills()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("skill %q not found in library", id)
}

func BundleEntries() (map[string][]BundleEntry, error) {
	bundles, err := readPersistedBundleEntries()
	if err != nil {
		return nil, err
	}
	skills, err := Skills()
	if err != nil {
		return nil, err
	}
	assigned := make(map[string]bool, len(skills))
	for name, ids := range bundles {
		if name == ReservedInboxBundle {
			continue
		}
		for _, id := range SourceIDsForBundleEntries(name, ids, skills) {
			assigned[id] = true
		}
	}
	var inbox []BundleEntry
	for _, skill := range skills {
		if assigned[skill.ID] {
			continue
		}
		inbox = append(inbox, BundleEntry{Skill: skill.ID})
	}
	if len(inbox) > 0 {
		bundles[ReservedInboxBundle] = inbox
	}
	return bundles, nil
}

func Bundles() (map[string][]string, error) {
	entries, err := BundleEntries()
	if err != nil {
		return nil, err
	}
	skills, err := Skills()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(entries))
	for name, ids := range entries {
		out[name] = SourceIDsForBundleEntries(name, ids, skills)
	}
	return out, nil
}

func SourceIDsForEntries(entries []BundleEntry) []string {
	return sourceIDsForEntries("", entries, nil)
}

func SourceIDsForBundleEntries(bundleName string, entries []BundleEntry, skills []Skill) []string {
	return sourceIDsForEntries(bundleName, entries, skillIDSet(skills))
}

func sourceIDsForEntries(bundleName string, entries []BundleEntry, known map[string]bool) []string {
	var out []string
	for _, entry := range entries {
		switch {
		case entry.Skill != "":
			out = append(out, resolveBundleRelativeID(bundleName, entry.Skill, known))
		case entry.Folder != "":
			if len(entry.Skills) > 0 {
				for _, skill := range entry.Skills {
					out = append(out, folderChildID(entry.Folder, skill))
				}
			} else {
				out = append(out, sourceIDsForFolder(entry.Folder)...)
			}
		}
	}
	return dedupSorted(out)
}

func skillIDSet(skills []Skill) map[string]bool {
	if len(skills) == 0 {
		return nil
	}
	out := make(map[string]bool, len(skills))
	for _, skill := range skills {
		out[skill.ID] = true
	}
	return out
}

func resolveBundleRelativeID(bundleName, id string, known map[string]bool) string {
	if bundleName == "" || strings.Contains(id, "/") {
		return id
	}
	candidate := bundleName + "/" + id
	if known != nil && known[candidate] {
		return candidate
	}
	return id
}

func BundleEntriesForSkills(ids []string) []BundleEntry {
	out := make([]BundleEntry, 0, len(ids))
	for _, id := range ids {
		out = append(out, BundleEntry{Skill: id})
	}
	return out
}

func readPersistedBundleEntries() (map[string][]BundleEntry, error) {
	path, err := BundlesPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string][]BundleEntry{}, nil
		}
		return nil, fmt.Errorf("reading bundles.yaml: %w", err)
	}
	var f bundleFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing bundles.yaml: %w", err)
	}
	if f.Bundles == nil {
		f.Bundles = map[string][]BundleEntry{}
	}
	return f.Bundles, nil
}

func WriteBundles(b map[string][]string) error {
	if err := EnsureLibrary(); err != nil {
		return err
	}

	current, _ := readPersistedBundleEntries()
	libSkills, err := Skills()
	if err != nil {
		return err
	}
	cleaned := make(map[string][]BundleEntry, len(b))
	for name, skillIDs := range b {
		if name == ReservedInboxBundle {
			continue
		}
		cleaned[name] = preserveStructuredEntries(name, current[name], dedupSorted(skillIDs), libSkills)
	}

	return WriteBundleEntries(cleaned)
}

func WriteBundleEntries(b map[string][]BundleEntry) error {
	if err := EnsureLibrary(); err != nil {
		return err
	}
	path, err := BundlesPath()
	if err != nil {
		return err
	}

	cleaned := make(map[string][]BundleEntry, len(b))
	for name, entries := range b {
		if name == ReservedInboxBundle {
			continue
		}
		cleaned[name] = entries
	}

	data, err := yaml.Marshal(bundleFile{Bundles: cleaned})
	if err != nil {
		return fmt.Errorf("marshaling bundles.yaml: %w", err)
	}
	header := "# skl bundles — edit by hand or via `skl bundle ...`\n\n"

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(header+string(data)), 0o644); err != nil {
		return fmt.Errorf("writing bundles.yaml: %w", err)
	}
	return os.Rename(tmp, path)
}

func preserveStructuredEntries(bundleName string, current []BundleEntry, wanted []string, skills []Skill) []BundleEntry {
	wantedSet := make(map[string]bool, len(wanted))
	for _, id := range wanted {
		wantedSet[id] = true
	}

	var out []BundleEntry
	for _, entry := range current {
		ids := SourceIDsForBundleEntries(bundleName, []BundleEntry{entry}, skills)
		if len(ids) == 0 || !allWanted(ids, wantedSet) {
			continue
		}
		out = append(out, entry)
		for _, id := range ids {
			delete(wantedSet, id)
		}
	}

	var remaining []string
	for id := range wantedSet {
		remaining = append(remaining, id)
	}
	sort.Strings(remaining)
	for _, id := range remaining {
		out = append(out, BundleEntry{Skill: id})
	}
	return out
}

func allWanted(ids []string, wanted map[string]bool) bool {
	for _, id := range ids {
		if !wanted[id] {
			return false
		}
	}
	return true
}

func hasSkillManifest(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}

func sourceIDsForFolder(folder string) []string {
	root, err := SkillsPath()
	if err != nil {
		return nil
	}
	folderRoot := filepath.Join(root, filepath.FromSlash(folder))
	var out []string
	err = filepath.WalkDir(folderRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != folderRoot && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "SKILL.md" {
			return nil
		}
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(folderRoot, dir)
		if err != nil {
			return nil
		}
		if rel == "." {
			out = append(out, folder)
			return nil
		}
		out = append(out, folder+"/"+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil
	}
	return dedupSorted(out)
}

func folderChildID(folder, skill string) string {
	skill = strings.Trim(strings.TrimSpace(skill), "/")
	if skill == "" || skill == "." {
		return folder
	}
	if strings.HasPrefix(skill, folder+"/") {
		return skill
	}
	return folder + "/" + skill
}

func dedupSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
