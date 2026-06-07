package library

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBundlesDerivesInboxAndWriteStripsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	skillsRoot, err := SkillsPath()
	if err != nil {
		t.Fatalf("SkillsPath: %v", err)
	}
	writeSkill(t, filepath.Join(skillsRoot, "alpha"))
	writeSkill(t, filepath.Join(skillsRoot, "beta"))
	writeSkill(t, filepath.Join(skillsRoot, "gamma"))

	if err := WriteBundles(map[string][]string{
		"dev":               {"alpha"},
		ReservedInboxBundle: {"beta"},
	}); err != nil {
		t.Fatalf("WriteBundles: %v", err)
	}

	bundles, err := Bundles()
	if err != nil {
		t.Fatalf("Bundles: %v", err)
	}
	want := map[string][]string{
		"dev":               {"alpha"},
		ReservedInboxBundle: {"beta", "gamma"},
	}
	if !reflect.DeepEqual(bundles, want) {
		t.Fatalf("Bundles mismatch\ngot:  %#v\nwant: %#v", bundles, want)
	}

	bundlesPath, err := BundlesPath()
	if err != nil {
		t.Fatalf("BundlesPath: %v", err)
	}
	data, err := os.ReadFile(bundlesPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", bundlesPath, err)
	}
	if strings.Contains(string(data), ReservedInboxBundle+":") {
		t.Fatalf("bundles.yaml should not persist %q:\n%s", ReservedInboxBundle, data)
	}
}

func TestBundlesParsesFolderEntryWithPrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	skillsRoot, err := SkillsPath()
	if err != nil {
		t.Fatalf("SkillsPath: %v", err)
	}
	writeSkill(t, filepath.Join(skillsRoot, "thinkhats", "1-blue-open"))
	writeSkill(t, filepath.Join(skillsRoot, "thinkhats", "2-white"))
	writeSkill(t, filepath.Join(skillsRoot, "plain"))

	bundlesPath, err := BundlesPath()
	if err != nil {
		t.Fatalf("BundlesPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(bundlesPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(bundlesPath), err)
	}
	data := `bundles:
  thinkhats:
    - folder: thinkhats
      prefix: thinkhats-nithin
      skills:
        - 1-blue-open
        - 2-white
`
	if err := os.WriteFile(bundlesPath, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", bundlesPath, err)
	}

	entries, err := BundleEntries()
	if err != nil {
		t.Fatalf("BundleEntries: %v", err)
	}
	gotEntries := entries["thinkhats"]
	wantEntries := []BundleEntry{{
		Folder: "thinkhats",
		Prefix: "thinkhats-nithin",
		Skills: []string{"1-blue-open", "2-white"},
	}}
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Fatalf("BundleEntries mismatch\ngot:  %#v\nwant: %#v", gotEntries, wantEntries)
	}

	bundles, err := Bundles()
	if err != nil {
		t.Fatalf("Bundles: %v", err)
	}
	wantBundles := map[string][]string{
		"thinkhats":         {"thinkhats/1-blue-open", "thinkhats/2-white"},
		ReservedInboxBundle: {"plain"},
	}
	if !reflect.DeepEqual(bundles, wantBundles) {
		t.Fatalf("Bundles mismatch\ngot:  %#v\nwant: %#v", bundles, wantBundles)
	}
}

func TestWriteBundlesPreservesUnchangedFolderEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	skillsRoot, err := SkillsPath()
	if err != nil {
		t.Fatalf("SkillsPath: %v", err)
	}
	writeSkill(t, filepath.Join(skillsRoot, "thinkhats", "1-blue-open"))
	writeSkill(t, filepath.Join(skillsRoot, "thinkhats", "2-white"))

	bundlesPath, err := BundlesPath()
	if err != nil {
		t.Fatalf("BundlesPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(bundlesPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(bundlesPath), err)
	}
	data := `bundles:
  thinkhats:
    - folder: thinkhats
      prefix: thinkhats-nithin
      skills:
        - 1-blue-open
        - 2-white
`
	if err := os.WriteFile(bundlesPath, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", bundlesPath, err)
	}

	bundles, err := Bundles()
	if err != nil {
		t.Fatalf("Bundles: %v", err)
	}
	if err := WriteBundles(bundles); err != nil {
		t.Fatalf("WriteBundles: %v", err)
	}

	written, err := os.ReadFile(bundlesPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", bundlesPath, err)
	}
	if !strings.Contains(string(written), "folder: thinkhats") {
		t.Fatalf("WriteBundles should preserve folder entry:\n%s", written)
	}
	if !strings.Contains(string(written), "prefix: thinkhats-nithin") {
		t.Fatalf("WriteBundles should preserve prefix entry:\n%s", written)
	}
}

func writeSkill(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}
}
