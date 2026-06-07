package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"skl/internal/bundle"
	"skl/internal/library"
	"skl/internal/live"
	"skl/internal/state"
)

func TestApplyLoadPlanReloadsTrackedSkill(t *testing.T) {
	setupHome(t)

	srcDir := filepath.Join(t.TempDir(), "foo-src")
	writeSkillTree(t, srcDir, "new")
	liveRoot, err := live.EnsureLive()
	if err != nil {
		t.Fatalf("EnsureLive: %v", err)
	}
	writeSkillTree(t, filepath.Join(liveRoot, "foo"), "old")

	st := &state.State{
		Version: 1,
		Loaded: map[string]state.LoadEntry{
			"foo": makeLoadEntry("foo", "old-src", "dev"),
		},
	}

	plan := makeLoadPlan("dev", library.Skill{ID: "foo", DirName: "foo", SrcPath: srcDir}, true)
	newCount, reloaded, err := applyLoadPlan(plan, st)
	if err != nil {
		t.Fatalf("applyLoadPlan: %v", err)
	}
	if newCount != 0 || reloaded != 1 {
		t.Fatalf("counts mismatch: new=%d reloaded=%d", newCount, reloaded)
	}
	if got := readSkillBody(t, filepath.Join(liveRoot, "foo")); got != "new" {
		t.Fatalf("live skill body = %q, want %q", got, "new")
	}
	entry := st.Loaded["foo"]
	if entry.Source != srcDir {
		t.Fatalf("state source = %q, want %q", entry.Source, srcDir)
	}
	if !reflect.DeepEqual(entry.Bundles, []string{"dev"}) {
		t.Fatalf("state bundles = %#v", entry.Bundles)
	}
}

func TestApplyLoadPlanOverwritesUntrackedDirAfterConfirmation(t *testing.T) {
	setupHome(t)

	srcDir := filepath.Join(t.TempDir(), "foo-src")
	writeSkillTree(t, srcDir, "fresh")
	liveRoot, err := live.EnsureLive()
	if err != nil {
		t.Fatalf("EnsureLive: %v", err)
	}
	writeSkillTree(t, filepath.Join(liveRoot, "foo"), "manual")

	st := &state.State{Version: 1, Loaded: map[string]state.LoadEntry{}}
	plan := makeLoadPlan("dev", library.Skill{ID: "foo", DirName: "foo", SrcPath: srcDir}, false)

	withStdin(t, "y\n", func() {
		_, _, err = applyLoadPlan(plan, st)
	})
	if err != nil {
		t.Fatalf("applyLoadPlan: %v", err)
	}
	if got := readSkillBody(t, filepath.Join(liveRoot, "foo")); got != "fresh" {
		t.Fatalf("live skill body = %q, want %q", got, "fresh")
	}
	if _, ok := st.Loaded["foo"]; !ok {
		t.Fatalf("state should contain newly loaded skill")
	}
}

func TestApplyLoadPlanReplacesConflictingLoadedDir(t *testing.T) {
	setupHome(t)

	srcDir := filepath.Join(t.TempDir(), "foo-src")
	writeSkillTree(t, srcDir, "replacement")
	liveRoot, err := live.EnsureLive()
	if err != nil {
		t.Fatalf("EnsureLive: %v", err)
	}
	writeSkillTree(t, filepath.Join(liveRoot, "foo"), "old")

	st := &state.State{
		Version: 1,
		Loaded: map[string]state.LoadEntry{
			"pack/foo": makeLoadEntry("foo", "pack-src", "pack"),
		},
	}
	plan := makeLoadPlan("dev", library.Skill{ID: "foo", DirName: "foo", SrcPath: srcDir}, false)

	withStdin(t, "y\n", func() {
		_, _, err = applyLoadPlan(plan, st)
	})
	if err != nil {
		t.Fatalf("applyLoadPlan: %v", err)
	}
	if got := readSkillBody(t, filepath.Join(liveRoot, "foo")); got != "replacement" {
		t.Fatalf("live skill body = %q, want %q", got, "replacement")
	}
	if _, ok := st.Loaded["pack/foo"]; ok {
		t.Fatalf("conflicting state entry should be removed")
	}
	if _, ok := st.Loaded["foo"]; !ok {
		t.Fatalf("replacement skill should be present in state")
	}
}

func TestApplyLoadPlanRestoresReloadedSkillOnFailure(t *testing.T) {
	setupHome(t)

	srcDir := filepath.Join(t.TempDir(), "foo-src")
	writeSkillTree(t, srcDir, "new")
	liveRoot, err := live.EnsureLive()
	if err != nil {
		t.Fatalf("EnsureLive: %v", err)
	}
	writeSkillTree(t, filepath.Join(liveRoot, "foo"), "old")

	st := &state.State{
		Version: 1,
		Loaded: map[string]state.LoadEntry{
			"foo": makeLoadEntry("foo", "old-src", "dev"),
		},
	}
	plan := bundle.LoadPlan{
		Bundle: "dev",
		Actions: []bundle.LoadAction{
			{
				Skill:   library.Skill{ID: "foo", DirName: "foo", SrcPath: srcDir},
				Already: true,
			},
			{
				Skill: library.Skill{ID: "bar", DirName: "bar", SrcPath: filepath.Join(t.TempDir(), "missing")},
			},
		},
	}

	if _, _, err := applyLoadPlan(plan, st); err == nil {
		t.Fatalf("applyLoadPlan should fail when a later copy fails")
	}
	if got := readSkillBody(t, filepath.Join(liveRoot, "foo")); got != "old" {
		t.Fatalf("live skill body after rollback = %q, want %q", got, "old")
	}
	entry := st.Loaded["foo"]
	if entry.Source != "old-src" {
		t.Fatalf("state source after rollback = %q, want %q", entry.Source, "old-src")
	}
	if !reflect.DeepEqual(entry.Bundles, []string{"dev"}) {
		t.Fatalf("state bundles after rollback = %#v", entry.Bundles)
	}
}

func TestLoadPlanExpandsRepoSkillWithBundlePrefix(t *testing.T) {
	setupHome(t)

	repoDir := filepath.Join(t.TempDir(), "gstack")
	writeNamedSkillTree(t, repoDir, "gstack", "root")
	writeNamedSkillTree(t, filepath.Join(repoDir, "autoplan"), "autoplan", "auto")
	writeNamedSkillTree(t, filepath.Join(repoDir, "gstack-upgrade"), "gstack-upgrade", "upgrade")

	st := &state.State{Version: 1, Loaded: map[string]state.LoadEntry{}}
	plan, err := bundle.PlanLoad("gstack-nithin", []string{"gstack-nithin"}, []library.Skill{
		{ID: "gstack-nithin", DirName: "gstack-nithin", SrcPath: repoDir},
	}, st)
	if err != nil {
		t.Fatalf("PlanLoad: %v", err)
	}
	gotActions := actionNames(plan.Actions)
	wantActions := []string{
		"gstack-nithin:gstack-nithin",
		"gstack-nithin-autoplan:gstack-nithin-autoplan",
		"gstack-nithin-upgrade:gstack-nithin-upgrade",
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("actions mismatch\ngot:  %#v\nwant: %#v", gotActions, wantActions)
	}

	newCount, reloaded, err := applyLoadPlan(plan, st)
	if err != nil {
		t.Fatalf("applyLoadPlan: %v", err)
	}
	if newCount != 3 || reloaded != 0 {
		t.Fatalf("counts mismatch: new=%d reloaded=%d", newCount, reloaded)
	}

	liveRoot, err := live.LivePath()
	if err != nil {
		t.Fatalf("LivePath: %v", err)
	}
	for _, name := range []string{"gstack-nithin", "gstack-nithin-autoplan", "gstack-nithin-upgrade"} {
		if got := readSkillName(t, filepath.Join(liveRoot, name)); got != name {
			t.Fatalf("%s copied manifest name = %q, want %q", name, got, name)
		}
		if _, ok := st.Loaded[name]; !ok {
			t.Fatalf("state missing loaded skill %q", name)
		}
	}
}

func TestLoadPlanLoadsFolderEntryWithPrefix(t *testing.T) {
	setupHome(t)

	skillsRoot, err := library.SkillsPath()
	if err != nil {
		t.Fatalf("SkillsPath: %v", err)
	}
	writeNamedSkillTree(t, filepath.Join(skillsRoot, "thinkhats", "1-blue-open"), "1-blue-open", "open")
	writeNamedSkillTree(t, filepath.Join(skillsRoot, "thinkhats", "2-white"), "2-white", "white")

	lib, err := library.Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	st := &state.State{Version: 1, Loaded: map[string]state.LoadEntry{}}
	plan, err := bundle.PlanLoadEntries("thinkhats", []library.BundleEntry{{
		Folder: "thinkhats",
		Prefix: "thinkhats-nithin",
		Skills: []string{"1-blue-open", "2-white"},
	}}, lib, st)
	if err != nil {
		t.Fatalf("PlanLoadEntries: %v", err)
	}
	gotActions := actionNames(plan.Actions)
	wantActions := []string{
		"thinkhats-nithin-1-blue-open:thinkhats-nithin-1-blue-open",
		"thinkhats-nithin-2-white:thinkhats-nithin-2-white",
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("actions mismatch\ngot:  %#v\nwant: %#v", gotActions, wantActions)
	}

	newCount, reloaded, err := applyLoadPlan(plan, st)
	if err != nil {
		t.Fatalf("applyLoadPlan: %v", err)
	}
	if newCount != 2 || reloaded != 0 {
		t.Fatalf("counts mismatch: new=%d reloaded=%d", newCount, reloaded)
	}

	liveRoot, err := live.LivePath()
	if err != nil {
		t.Fatalf("LivePath: %v", err)
	}
	for _, name := range []string{"thinkhats-nithin-1-blue-open", "thinkhats-nithin-2-white"} {
		if got := readSkillName(t, filepath.Join(liveRoot, name)); got != name {
			t.Fatalf("%s copied manifest name = %q, want %q", name, got, name)
		}
	}
	if _, err := os.Stat(filepath.Join(liveRoot, "thinkhats-nithin")); !os.IsNotExist(err) {
		t.Fatalf("parent skill should not be loaded, stat err = %v", err)
	}
}

func TestLoadPlanInfersBundleFolderForSkillEntries(t *testing.T) {
	setupHome(t)

	skillsRoot, err := library.SkillsPath()
	if err != nil {
		t.Fatalf("SkillsPath: %v", err)
	}
	writeNamedSkillTree(t, filepath.Join(skillsRoot, "dev", "dev1-start"), "dev1-start", "start")
	writeNamedSkillTree(t, filepath.Join(skillsRoot, "dev", "dev2-design"), "dev2-design", "design")

	lib, err := library.Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	st := &state.State{Version: 1, Loaded: map[string]state.LoadEntry{}}
	plan, err := bundle.PlanLoadEntries("dev", []library.BundleEntry{
		{Skill: "dev1-start"},
		{Skill: "dev2-design"},
	}, lib, st)
	if err != nil {
		t.Fatalf("PlanLoadEntries: %v", err)
	}
	gotActions := actionNames(plan.Actions)
	wantActions := []string{
		"dev/dev1-start:dev1-start",
		"dev/dev2-design:dev2-design",
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("actions mismatch\ngot:  %#v\nwant: %#v", gotActions, wantActions)
	}
}

func TestLoadPlanAppliesPrefixDirectiveToBundleRelativeSkills(t *testing.T) {
	setupHome(t)

	skillsRoot, err := library.SkillsPath()
	if err != nil {
		t.Fatalf("SkillsPath: %v", err)
	}
	writeNamedSkillTree(t, filepath.Join(skillsRoot, "thinkhats", "1-blue-open"), "1-blue-open", "open")
	writeNamedSkillTree(t, filepath.Join(skillsRoot, "thinkhats", "2-white"), "2-white", "white")

	lib, err := library.Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	st := &state.State{Version: 1, Loaded: map[string]state.LoadEntry{}}
	plan, err := bundle.PlanLoadEntries("thinkhats", []library.BundleEntry{
		{Prefix: "thinkhats-nithin"},
		{Skill: "1-blue-open"},
		{Skill: "2-white"},
	}, lib, st)
	if err != nil {
		t.Fatalf("PlanLoadEntries: %v", err)
	}
	gotActions := actionNames(plan.Actions)
	wantActions := []string{
		"thinkhats-nithin-1-blue-open:thinkhats-nithin-1-blue-open",
		"thinkhats-nithin-2-white:thinkhats-nithin-2-white",
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("actions mismatch\ngot:  %#v\nwant: %#v", gotActions, wantActions)
	}

	newCount, reloaded, err := applyLoadPlan(plan, st)
	if err != nil {
		t.Fatalf("applyLoadPlan: %v", err)
	}
	if newCount != 2 || reloaded != 0 {
		t.Fatalf("counts mismatch: new=%d reloaded=%d", newCount, reloaded)
	}

	liveRoot, err := live.LivePath()
	if err != nil {
		t.Fatalf("LivePath: %v", err)
	}
	for _, name := range []string{"thinkhats-nithin-1-blue-open", "thinkhats-nithin-2-white"} {
		if got := readSkillName(t, filepath.Join(liveRoot, name)); got != name {
			t.Fatalf("%s copied manifest name = %q, want %q", name, got, name)
		}
	}
}

func setupHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func makeLoadPlan(bundleName string, skill library.Skill, already bool) bundle.LoadPlan {
	return bundle.LoadPlan{
		Bundle: bundleName,
		Actions: []bundle.LoadAction{
			{
				Skill:   skill,
				Already: already,
			},
		},
	}
}

func makeLoadEntry(dirName, source string, bundles ...string) state.LoadEntry {
	return state.LoadEntry{
		DirName:  dirName,
		Source:   source,
		Bundles:  bundles,
		LoadedAt: time.Unix(123, 0).UTC(),
	}
}

func writeSkillTree(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "body.txt"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(body.txt): %v", err)
	}
}

func writeNamedSkillTree(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	manifest := "---\nname: " + name + "\n---\n# skill\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "body.txt"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(body.txt): %v", err)
	}
}

func readSkillBody(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "body.txt"))
	if err != nil {
		t.Fatalf("ReadFile(body.txt): %v", err)
	}
	return string(data)
}

func readSkillName(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("ReadFile(SKILL.md): %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "name: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name: "))
		}
	}
	t.Fatalf("SKILL.md in %s has no name field", dir)
	return ""
}

func actionNames(actions []bundle.LoadAction) []string {
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		out = append(out, action.Skill.ID+":"+action.Skill.DirName)
	}
	return out
}

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("WriteString(stdin): %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close(stdin writer): %v", err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()
	fn()
}
