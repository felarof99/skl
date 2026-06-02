package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"skl/internal/gitlib"
	"skl/internal/library"
	"skl/internal/style"

	"github.com/spf13/cobra"
)

func init() {
	installCmd.Flags().String("bundle", "", "Add imported skills to this bundle (creates if absent)")
	installCmd.Flags().String("name", "", "Override namespace name (namespaced mode only)")
	installCmd.Flags().String("subdir", "", "Scan this subdirectory of the source (e.g., 'skills')")
	installCmd.Flags().String("prefix", "", "Install flat as library/skills/<prefix>-<skill>/ (instead of a pack at library/skills/<ns>/<skill>/)")
	installCmd.Flags().Bool("force", false, "Overwrite existing skills / namespaces")
	installCmd.Flags().Bool("no-bundle", false, "Don't auto-add installed skills to a bundle (leave them in inbox)")
	rootCmd.AddCommand(installCmd)
}

var installCmd = &cobra.Command{
	Use:         "install <git-url | path>",
	Annotations: map[string]string{"group": "Library:"},
	Short:       "Import skills from a git URL or local path",
	Long: `Import third-party skills into the library from a git URL or a local path.

Two install modes:

  Pack (default)
      skl install https://github.com/obra/superpowers --subdir skills
      → installs to library/skills/superpowers/ as a pack, skills referenced
        as superpowers/<skill> in bundles.yaml.

  Flat with prefix
      skl install /path/to/repo --subdir skills --prefix supa --bundle sp
      → copies each skill into library/skills/supa-<skill>/, so they appear
        as native skills. Bundle 'sp' lists them as 'supa-<skill>'.

By default the installed skills are added to a bundle named after the source
(the namespace, or the --prefix), so they show up immediately in 'skl load'.
Use --bundle to pick a different/existing bundle, or --no-bundle to leave them
in the 'inbox' catch-all.

Flags:
  --subdir <path>   Scan a subdirectory of the source for skills (many repos
                    nest skills under 'skills/').
  --prefix <name>   Flatten into library/skills/ with <prefix>- on each name.
  --bundle <name>   Add all imported skills to this bundle (default: source name).
  --no-bundle       Don't auto-bundle; leave installed skills in inbox.
  --name <name>     Override the namespace dir name (no effect with --prefix).
  --force           Overwrite an existing namespace or prefixed skill.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		bundleName, _ := cmd.Flags().GetString("bundle")
		nameOverride, _ := cmd.Flags().GetString("name")
		subdir, _ := cmd.Flags().GetString("subdir")
		prefix, _ := cmd.Flags().GetString("prefix")
		force, _ := cmd.Flags().GetBool("force")
		noBundle, _ := cmd.Flags().GetBool("no-bundle")
		if bundleName != "" {
			if err := rejectReservedBundle(bundleName); err != nil {
				return err
			}
		}

		src := args[0]
		isLocal := looksLocal(src)

		rootDir, tmpDir, nsFromClone, err := resolveInstallSource(src, isLocal, prefix, subdir, nameOverride, force)
		if err != nil {
			return err
		}
		if tmpDir != "" {
			defer os.RemoveAll(tmpDir)
		}

		scanDir := rootDir
		if subdir != "" {
			scanDir = filepath.Join(rootDir, subdir)
		}

		skillSrcs, err := findSkillDirs(scanDir)
		if err != nil {
			return err
		}
		if len(skillSrcs) == 0 {
			return fmt.Errorf("no skills (dirs containing SKILL.md) found in %s", scanDir)
		}

		var added []string
		var nsUsed string
		if prefix != "" {
			added, err = installFlatPrefixed(skillSrcs, prefix, force)
		} else {
			added, nsUsed, err = installNamespaced(skillSrcs, nsFromClone, isLocal, src, nameOverride, force)
		}
		if err != nil {
			return err
		}

		sort.Strings(added)
		fmt.Printf("%s %d skill(s)\n", style.OK("installed"), len(added))
		for _, id := range added {
			fmt.Printf("  %s\n", id)
		}

		// Auto-bundle so the skills show up in `skl load` right away.
		effBundle := bundleName
		if effBundle == "" && !noBundle {
			effBundle = deriveBundleName(prefix, nsUsed)
		}
		if effBundle != "" && len(added) > 0 {
			if err := addSkillsToBundle(effBundle, added); err != nil {
				return err
			}
			fmt.Printf("%s skills to bundle %q\n", style.OK("added"), effBundle)
			fmt.Printf("%s run %s to activate\n", style.Faint("→"), style.Cmd("skl load "+effBundle))
		} else if len(added) > 0 {
			// Not bundled: they sit in the inbox catch-all.
			fmt.Printf("%s skills are in %s — run %s to bundle them\n",
				style.Faint("→"), style.Faint("inbox"), style.Cmd("skl bundle add <name> <skill>"))
		}
		return nil
	},
}

// deriveBundleName picks the default bundle for an install when --bundle is
// absent: the --prefix if flat, else the namespace dir. Sanitized; returns ""
// when it can't or shouldn't auto-bundle (empty, or the reserved inbox name).
func deriveBundleName(prefix, namespace string) string {
	raw := prefix
	if raw == "" {
		raw = namespace
	}
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.ReplaceAll(name, " ", "-")
	if name == library.ReservedInboxBundle {
		return ""
	}
	return name
}

// addSkillsToBundle appends ids to a bundle (creating it if absent), dedup via WriteBundles.
func addSkillsToBundle(name string, ids []string) error {
	bundles, err := library.Bundles()
	if err != nil {
		return err
	}
	merged := append([]string{}, bundles[name]...)
	merged = append(merged, ids...)
	bundles[name] = merged
	return library.WriteBundles(bundles)
}

// resolveInstallSource returns the root directory to scan, an optional temp dir
// to clean up, and — in the namespaced-clone-direct case — the namespace name
// the clone landed at.
func resolveInstallSource(src string, isLocal bool, prefix, subdir, nameOverride string, force bool) (rootDir, tmpDir, nsFromClone string, err error) {
	if isLocal {
		abs, err := filepath.Abs(src)
		if err != nil {
			return "", "", "", err
		}
		return abs, "", "", nil
	}

	directClone := prefix == "" && subdir == ""
	if directClone {
		ns := nameOverride
		if ns == "" {
			ns = repoNameFromURL(src)
		}
		if ns == "" {
			return "", "", "", fmt.Errorf("could not derive a namespace from %q (use --name)", src)
		}
		extDir, err := library.SkillsPath()
		if err != nil {
			return "", "", "", err
		}
		if err := os.MkdirAll(extDir, 0o755); err != nil {
			return "", "", "", err
		}
		dest := filepath.Join(extDir, ns)
		if _, err := os.Stat(dest); err == nil {
			if !force {
				return "", "", "", fmt.Errorf("namespace %q already exists; use --force or --name", ns)
			}
			if err := os.RemoveAll(dest); err != nil {
				return "", "", "", err
			}
		}
		if err := gitlib.Clone(src, dest); err != nil {
			return "", "", "", err
		}
		return dest, "", ns, nil
	}

	tmp, err := os.MkdirTemp("", "skl-install-*")
	if err != nil {
		return "", "", "", err
	}
	cloneDest := filepath.Join(tmp, "repo")
	if err := gitlib.Clone(src, cloneDest); err != nil {
		os.RemoveAll(tmp)
		return "", "", "", err
	}
	return cloneDest, tmp, "", nil
}

func installFlatPrefixed(skillSrcs []string, prefix string, force bool) ([]string, error) {
	skillsDir, err := library.SkillsPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, err
	}

	var added []string
	for _, sd := range skillSrcs {
		orig := filepath.Base(sd)
		newName := prefix + "-" + orig
		dst := filepath.Join(skillsDir, newName)
		if _, err := os.Stat(dst); err == nil {
			if !force {
				fmt.Fprintf(os.Stderr, "skl: skip %s (exists; use --force to overwrite)\n", newName)
				continue
			}
			if err := os.RemoveAll(dst); err != nil {
				return nil, err
			}
		}
		if err := copyDir(sd, dst); err != nil {
			return nil, fmt.Errorf("copying %s: %w", orig, err)
		}
		added = append(added, newName)
	}
	return added, nil
}

func installNamespaced(skillSrcs []string, nsFromClone string, isLocal bool, src, nameOverride string, force bool) ([]string, string, error) {
	if nsFromClone != "" {
		var added []string
		for _, sd := range skillSrcs {
			added = append(added, nsFromClone+"/"+filepath.Base(sd))
		}
		return added, nsFromClone, nil
	}

	ns := nameOverride
	if ns == "" {
		if isLocal {
			abs, err := filepath.Abs(src)
			if err != nil {
				return nil, "", err
			}
			ns = filepath.Base(abs)
		} else {
			ns = repoNameFromURL(src)
		}
	}
	if ns == "" {
		return nil, "", fmt.Errorf("could not derive a namespace (use --name)")
	}

	extDir, err := library.SkillsPath()
	if err != nil {
		return nil, "", err
	}
	nsDir := filepath.Join(extDir, ns)
	if _, err := os.Stat(nsDir); err == nil {
		if !force {
			return nil, "", fmt.Errorf("namespace %q already exists; use --force or --name", ns)
		}
		if err := os.RemoveAll(nsDir); err != nil {
			return nil, "", err
		}
	}
	if err := os.MkdirAll(nsDir, 0o755); err != nil {
		return nil, "", err
	}

	var added []string
	for _, sd := range skillSrcs {
		name := filepath.Base(sd)
		dst := filepath.Join(nsDir, name)
		if err := copyDir(sd, dst); err != nil {
			return nil, "", fmt.Errorf("copying %s: %w", name, err)
		}
		added = append(added, ns+"/"+name)
	}
	return added, ns, nil
}

func findSkillDirs(root string) ([]string, error) {
	if info, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("scan dir %s: %w", root, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(sub, "SKILL.md")); err != nil {
			continue
		}
		out = append(out, sub)
	}
	sort.Strings(out)
	return out, nil
}

func looksLocal(src string) bool {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") ||
		strings.HasPrefix(src, "git@") || strings.HasPrefix(src, "git://") ||
		strings.HasPrefix(src, "ssh://") {
		return false
	}
	info, err := os.Stat(src)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func repoNameFromURL(url string) string {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	if idx := strings.LastIndexAny(url, "/:"); idx >= 0 {
		return url[idx+1:]
	}
	return url
}
