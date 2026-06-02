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
	ID       string
	DirName  string
	SrcPath  string
	External bool
	Repo     string
}

const ReservedInboxBundle = "inbox"

type bundleFile struct {
	Bundles map[string][]string `yaml:"bundles"`
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

func Bundles() (map[string][]string, error) {
	bundles, err := readPersistedBundles()
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
		for _, id := range ids {
			assigned[id] = true
		}
	}
	var inbox []string
	for _, skill := range skills {
		if assigned[skill.ID] {
			continue
		}
		inbox = append(inbox, skill.ID)
	}
	if len(inbox) > 0 {
		bundles[ReservedInboxBundle] = inbox
	}
	return bundles, nil
}

func readPersistedBundles() (map[string][]string, error) {
	path, err := BundlesPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string][]string{}, nil
		}
		return nil, fmt.Errorf("reading bundles.yaml: %w", err)
	}
	var f bundleFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing bundles.yaml: %w", err)
	}
	if f.Bundles == nil {
		f.Bundles = map[string][]string{}
	}
	return f.Bundles, nil
}

func WriteBundles(b map[string][]string) error {
	if err := EnsureLibrary(); err != nil {
		return err
	}
	path, err := BundlesPath()
	if err != nil {
		return err
	}

	cleaned := make(map[string][]string, len(b))
	for name, skills := range b {
		if name == ReservedInboxBundle {
			continue
		}
		cleaned[name] = dedupSorted(skills)
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

func hasSkillManifest(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
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
