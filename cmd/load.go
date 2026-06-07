package cmd

import (
	"fmt"
	"sort"
	"strings"

	"skl/internal/bundle"
	"skl/internal/library"
	"skl/internal/live"
	"skl/internal/picker"
	"skl/internal/state"
	"skl/internal/style"

	"github.com/spf13/cobra"
)

func init() {
	loadCmd.Flags().StringSlice("skill", nil, "Load individual skill(s) directly (repeatable)")
	rootCmd.AddCommand(loadCmd)
}

var loadCmd = &cobra.Command{
	Use:         "load [bundle...]",
	Aliases:     []string{"l"},
	Annotations: map[string]string{"group": "Load:"},
	Short:       "Load bundles into ~/.skills/ (fzf when no args)",
	Long: `Copy skills from the library into ~/.skills/. Re-running load refreshes
managed skills from the current library version. If an existing live skill
dir would be replaced for some other reason, load asks before overwriting it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		individualSkills, _ := cmd.Flags().GetStringSlice("skill")

		bundles, err := library.Bundles()
		if err != nil {
			return err
		}
		bundleEntries, err := library.BundleEntries()
		if err != nil {
			return err
		}
		lib, err := library.Skills()
		if err != nil {
			return err
		}

		chosen := args
		if len(chosen) == 0 && len(individualSkills) == 0 {
			chosen, err = pickBundles(bundles, "load > ")
			if err != nil {
				return err
			}
		}

		mgr, err := state.NewManager()
		if err != nil {
			return err
		}
		if err := mgr.Lock(); err != nil {
			return err
		}
		defer mgr.Unlock()

		st, err := mgr.Load()
		if err != nil {
			return err
		}

		totalNew, totalReloaded := 0, 0

		for _, name := range chosen {
			entries, ok := bundleEntries[name]
			if !ok {
				return fmt.Errorf("bundle %q not found", name)
			}
			plan, err := bundle.PlanLoadEntries(name, entries, lib, st)
			if err != nil {
				return err
			}
			newCount, reloaded, err := applyLoadPlan(plan, st)
			if err != nil {
				return fmt.Errorf("loading bundle %q: %w", name, err)
			}
			totalNew += newCount
			totalReloaded += reloaded
			if reloaded > 0 {
				fmt.Printf("%s bundle %q  %s %d new  %s %d reloaded\n",
					style.OK("loaded"), name,
					style.Faint("+"), newCount,
					style.Faint("↻"), reloaded)
			} else {
				fmt.Printf("%s bundle %q  %s %d skill(s)\n",
					style.OK("loaded"), name,
					style.Faint("+"), newCount)
			}
		}

		for _, id := range individualSkills {
			s, err := library.FindSkill(id)
			if err != nil {
				return err
			}
			synthetic := bundle.LoadPlan{
				Bundle:  "__skill__",
				Actions: []bundle.LoadAction{{Skill: *s, Already: stateHas(st, id)}},
			}
			newCount, reloaded, err := applyLoadPlan(synthetic, st)
			if err != nil {
				return fmt.Errorf("loading skill %q: %w", id, err)
			}
			totalNew += newCount
			totalReloaded += reloaded
			fmt.Printf("%s skill %q\n", style.OK("loaded"), id)
		}

		if err := mgr.Save(st); err != nil {
			return err
		}
		total := totalNew + totalReloaded
		fmt.Printf("\n%s %d skill(s)", style.OK("done:"), total)
		if totalReloaded > 0 {
			fmt.Printf("  %s %d reloaded", style.Faint("↻"), totalReloaded)
		}
		fmt.Println()
		return nil
	},
}

type loadRollback struct {
	skillID       string
	dirName       string
	backups       []live.Backup
	previousState map[string]state.LoadEntry
}

func applyLoadPlan(plan bundle.LoadPlan, st *state.State) (newCount, reloaded int, err error) {
	var applied []loadRollback
	for _, action := range plan.Actions {
		existingOnDisk, err := live.SkillExists(action.Skill.DirName)
		if err != nil {
			rollbackLoadPlan(applied, st)
			return 0, 0, err
		}
		claimedIDs := loadedIDsForDir(st, action.Skill.DirName)
		if existingOnDisk && !isSafeAutoReload(claimedIDs, action.Skill.ID) {
			if !confirm(loadReplacePrompt(action.Skill, claimedIDs)) {
				rollbackLoadPlan(applied, st)
				return 0, 0, ErrCancelled
			}
		}

		rollback := loadRollback{
			skillID:       action.Skill.ID,
			dirName:       action.Skill.DirName,
			previousState: snapshotLoadState(st, action.Skill.ID, action.Skill.DirName),
		}
		if existingOnDisk {
			rollback.backups, err = live.BackupSkill(action.Skill.DirName)
			if err != nil {
				rollbackLoadPlan(applied, st)
				return 0, 0, err
			}
		}

		if err := live.CopySkillWithName(action.Skill.SrcPath, action.Skill.DirName, action.Skill.NameOverride); err != nil {
			_ = live.RemoveSkill(action.Skill.DirName)
			if restoreErr := live.RestoreSkill(rollback.backups); restoreErr != nil {
				rollbackLoadPlan(applied, st)
				return 0, 0, fmt.Errorf("%w (restore failed: %v)", err, restoreErr)
			}
			rollbackLoadPlan(applied, st)
			return 0, 0, err
		}

		removeLoadedByDirExcept(st, action.Skill.DirName, action.Skill.ID)
		st.AddBundleClaim(action.Skill.ID, action.Skill.DirName, action.Skill.SrcPath, plan.Bundle)
		applied = append(applied, rollback)
		if action.Already {
			reloaded++
		} else {
			newCount++
		}
	}
	cleanupLoadBackups(applied)
	return newCount, reloaded, nil
}

func rollbackLoadPlan(applied []loadRollback, st *state.State) {
	for i := len(applied) - 1; i >= 0; i-- {
		step := applied[i]
		_ = live.RemoveSkill(step.dirName)
		if err := live.RestoreSkill(step.backups); err == nil {
			restoreLoadState(st, step)
		}
	}
}

func stateHas(st *state.State, id string) bool {
	_, ok := st.Loaded[id]
	return ok
}

func cleanupLoadBackups(applied []loadRollback) {
	for _, step := range applied {
		live.CleanupBackups(step.backups)
	}
}

func snapshotLoadState(st *state.State, skillID, dirName string) map[string]state.LoadEntry {
	out := map[string]state.LoadEntry{}
	if entry, ok := st.Loaded[skillID]; ok {
		out[skillID] = entry
	}
	for id, entry := range st.Loaded {
		if entry.DirName == dirName {
			out[id] = entry
		}
	}
	return out
}

func restoreLoadState(st *state.State, step loadRollback) {
	clearIDs := map[string]bool{step.skillID: true}
	for id := range step.previousState {
		clearIDs[id] = true
	}
	for id, entry := range st.Loaded {
		if entry.DirName == step.dirName {
			clearIDs[id] = true
		}
	}
	for id := range clearIDs {
		delete(st.Loaded, id)
	}
	for id, entry := range step.previousState {
		st.Loaded[id] = entry
	}
}

func removeLoadedByDirExcept(st *state.State, dirName, keepID string) {
	for id, entry := range st.Loaded {
		if entry.DirName == dirName && id != keepID {
			st.RemoveLoaded(id)
		}
	}
}

func loadedIDsForDir(st *state.State, dirName string) []string {
	var ids []string
	for id, entry := range st.Loaded {
		if entry.DirName == dirName {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func isSafeAutoReload(claimedIDs []string, skillID string) bool {
	return len(claimedIDs) == 1 && claimedIDs[0] == skillID
}

func loadReplacePrompt(skill library.Skill, claimedIDs []string) string {
	if len(claimedIDs) == 0 {
		return fmt.Sprintf("Skill dir %q already exists in ~/.skills/. Replace it with %q from the library?", skill.DirName, skill.ID)
	}
	if len(claimedIDs) == 1 {
		return fmt.Sprintf("Skill dir %q is currently used by loaded skill %q. Replace it with %q?", skill.DirName, claimedIDs[0], skill.ID)
	}
	return fmt.Sprintf("Skill dir %q is currently used by loaded skills %s. Replace it with %q?", skill.DirName, strings.Join(claimedIDs, ", "), skill.ID)
}

func pickBundles(bundles map[string][]string, prompt string) ([]string, error) {
	if len(bundles) == 0 {
		return nil, fmt.Errorf("no bundles defined")
	}
	var items []picker.Item
	for name, skills := range bundles {
		items = append(items, picker.Item{
			ID:      name,
			Display: fmt.Sprintf("%s\t(%d skills)", name, len(skills)),
		})
	}
	chosen, err := picker.Pick(items, picker.Opts{Prompt: prompt, Multi: true})
	if err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		return nil, ErrCancelled
	}
	return chosen, nil
}
