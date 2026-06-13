package cmd

import (
	"path/filepath"
	"reflect"
	"testing"

	"skl/internal/bundle"
	"skl/internal/library"
	"skl/internal/state"
)

func TestBundleSkillRowsUsesExpandedFolderEntries(t *testing.T) {
	setupHome(t)

	skillsRoot, err := library.SkillsPath()
	if err != nil {
		t.Fatalf("SkillsPath: %v", err)
	}
	writeNamedSkillTree(t, filepath.Join(skillsRoot, "famous-mskill", "skills", "ab-testing"), "ab-testing", "ab")
	writeNamedSkillTree(t, filepath.Join(skillsRoot, "famous-mskill", "skills", "ads"), "ads", "ads")

	lib, err := library.Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	st := &state.State{Version: 1, Loaded: map[string]state.LoadEntry{}}
	plan, err := bundle.PlanLoadEntries("famous-mskill", []library.BundleEntry{{
		Folder: "famous-mskill",
		Prefix: "famous-mskill",
	}}, lib, st)
	if err != nil {
		t.Fatalf("PlanLoadEntries: %v", err)
	}

	rows := bundleSkillRows(plan.Actions, st)
	var got []string
	for _, row := range rows {
		got = append(got, row[0])
		if row[2] == "missing" {
			t.Fatalf("recursive folder row should not be missing: %#v", row)
		}
	}
	want := []string{"famous-mskill-ab-testing", "famous-mskill-ads"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}
