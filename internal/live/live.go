package live

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"skl/internal/config"
)

// LivePaths returns the configured live directories, defaulting to ~/.skills
// when none are configured. Paths starting with "~/" are expanded.
func LivePaths() ([]string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if len(cfg.LiveDirs) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		return []string{filepath.Join(home, ".skills")}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cfg.LiveDirs))
	for _, p := range cfg.LiveDirs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "~/") {
			p = filepath.Join(home, p[2:])
		} else if p == "~" {
			p = home
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("config live_dirs has no usable entries")
	}
	return out, nil
}

// LivePath returns the first configured live directory. Most callers should
// prefer LivePaths or LocateSkill; this exists for legacy single-dir use.
func LivePath() (string, error) {
	paths, err := LivePaths()
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

// EnsureLivePaths mkdir-ps every configured live dir and returns the list.
func EnsureLivePaths() ([]string, error) {
	paths, err := LivePaths()
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// EnsureLive mkdir-ps every live dir, returning the first one for callers
// that only need a single path (e.g. source for import / push).
func EnsureLive() (string, error) {
	paths, err := EnsureLivePaths()
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

// LoadedDirs returns the union of skill directory names across all live dirs,
// skipping dot-prefixed entries.
func LoadedDirs() ([]string, error) {
	paths, err := LivePaths()
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range paths {
		entries, err := os.ReadDir(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			set[e.Name()] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// LocateSkill returns the first live path that contains <dirName>, or empty
// if the skill is not present anywhere.
func LocateSkill(dirName string) (string, error) {
	if err := guardDirName(dirName); err != nil {
		return "", err
	}
	paths, err := LivePaths()
	if err != nil {
		return "", err
	}
	for _, p := range paths {
		info, err := os.Stat(filepath.Join(p, dirName))
		if err == nil && info.IsDir() {
			return p, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

// SkillExists reports whether <dirName> exists in any live dir.
func SkillExists(dirName string) (bool, error) {
	root, err := LocateSkill(dirName)
	if err != nil {
		return false, err
	}
	return root != "", nil
}

// CopySkill recursively copies srcDir into <livePath>/<dirName> for every
// configured live dir. Rolls back partial copies on any failure.
func CopySkill(srcDir, dirName string) error {
	return CopySkillWithName(srcDir, dirName, "")
}

// CopySkillWithName is CopySkill plus an optional copied-manifest name rewrite.
// The source tree is not modified.
func CopySkillWithName(srcDir, dirName, manifestName string) error {
	if err := guardDirName(dirName); err != nil {
		return err
	}
	paths, err := EnsureLivePaths()
	if err != nil {
		return err
	}

	var written []string
	for _, p := range paths {
		dst := filepath.Join(p, dirName)
		if _, err := os.Stat(dst); err == nil {
			rollback(written)
			return fmt.Errorf("skill %q already exists in %s; backup before copy", dirName, p)
		}
		if err := copyTree(srcDir, dst, manifestName); err != nil {
			_ = os.RemoveAll(dst)
			rollback(written)
			return err
		}
		written = append(written, dst)
	}
	return nil
}

// RemoveSkill deletes <dirName> from every live dir that contains it.
func RemoveSkill(dirName string) error {
	if err := guardDirName(dirName); err != nil {
		return err
	}
	paths, err := LivePaths()
	if err != nil {
		return err
	}
	for _, p := range paths {
		target := filepath.Join(p, dirName)
		if _, err := os.Stat(target); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	return nil
}

// Backup represents a snapshot of <dirName> in one live dir, renamed aside so
// CopySkill can write fresh into the same location. Restore puts it back.
type Backup struct {
	LivePath   string
	DirName    string
	BackupPath string
}

// BackupSkill renames <dirName> out of every live dir that contains it,
// returning the backup paths. On any failure the prior renames are rolled
// back before returning.
func BackupSkill(dirName string) ([]Backup, error) {
	if err := guardDirName(dirName); err != nil {
		return nil, err
	}
	paths, err := LivePaths()
	if err != nil {
		return nil, err
	}

	var backups []Backup
	for _, p := range paths {
		target := filepath.Join(p, dirName)
		if _, err := os.Stat(target); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			_ = RestoreSkill(backups)
			return nil, err
		}
		tmp, err := os.MkdirTemp(p, "."+dirName+".skl-backup-*")
		if err != nil {
			_ = RestoreSkill(backups)
			return nil, err
		}
		if err := os.RemoveAll(tmp); err != nil {
			_ = RestoreSkill(backups)
			return nil, err
		}
		if err := os.Rename(target, tmp); err != nil {
			_ = RestoreSkill(backups)
			return nil, err
		}
		backups = append(backups, Backup{LivePath: p, DirName: dirName, BackupPath: tmp})
	}
	return backups, nil
}

// RestoreSkill returns each backup to its original spot, overwriting whatever
// is currently there. Used as the rollback for a failed CopySkill.
func RestoreSkill(backups []Backup) error {
	for _, b := range backups {
		target := filepath.Join(b.LivePath, b.DirName)
		_ = os.RemoveAll(target)
		if err := os.Rename(b.BackupPath, target); err != nil {
			return err
		}
	}
	return nil
}

// CleanupBackups removes backup directories after a successful copy.
func CleanupBackups(backups []Backup) {
	for _, b := range backups {
		_ = os.RemoveAll(b.BackupPath)
	}
}

func rollback(written []string) {
	for _, w := range written {
		_ = os.RemoveAll(w)
	}
}

func guardDirName(name string) error {
	if name == "" {
		return errors.New("empty skill name")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("refusing to operate on dot-prefixed entry %q", name)
	}
	if strings.ContainsRune(name, filepath.Separator) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid skill name %q", name)
	}
	return nil
}

// copyTree recursively copies src into dst. Symlinks are skipped — skills can
// come from untrusted git repos via `skl install`, and a malicious symlink
// could otherwise cause reads outside the skill directory.
func copyTree(src, dst, manifestName string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "skl: skipping symlink %s\n", path)
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if manifestName != "" && rel == "SKILL.md" {
			return copySkillManifestWithName(path, target, info.Mode().Perm(), manifestName)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copySkillManifestWithName(src, dst string, mode os.FileMode, name string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(rewriteManifestName(string(data), name)), mode)
}

func rewriteManifestName(content, name string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" {
			lines = append(lines[:1], append([]string{"name: " + name}, lines[1:]...)...)
			return strings.Join(lines, "\n")
		}
		if strings.HasPrefix(trimmed, "name:") {
			lines[i] = "name: " + name
			return strings.Join(lines, "\n")
		}
	}
	return content
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
