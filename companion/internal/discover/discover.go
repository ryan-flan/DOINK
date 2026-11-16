// Package discover finds DOINK's SavedVariables files so config.toml can
// stay optional.
//
// It looks in the usual World of Warcraft install folders on every drive
// (Windows), every /mnt/<letter> (WSL) and every Wine/Proton prefix's
// drive_c (Lutris, Steam, plain Wine), and in every flavor folder
// (_forever_, _classic_beta_, ...) that has the DOINK addon installed, for
// each account under WTF/Account. On macOS it looks in /Applications,
// ~/Applications and every mounted volume.
package discover

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// WowDirs are the install folders tried under each drive root.
var WowDirs = []string{
	"Program Files (x86)/World of Warcraft",
	"Program Files/World of Warcraft",
	"World of Warcraft",
	"Games/World of Warcraft",
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// DriveRoots lists the places a WoW install folder may hang off.
func DriveRoots() []string {
	if runtime.GOOS == "windows" {
		var roots []string
		for letter := 'C'; letter <= 'Z'; letter++ {
			root := string(letter) + `:\`
			if isDir(root) {
				roots = append(roots, root)
			}
		}
		return roots
	}
	var roots []string
	seen := map[string]bool{}
	add := func(path string) {
		key := path
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			key = resolved // /Volumes/Macintosh HD is a link to /
		}
		if !seen[key] && isDir(path) {
			seen[key] = true
			roots = append(roots, path)
		}
	}
	if runtime.GOOS == "darwin" {
		// The Mac client installs to /Applications/World of Warcraft;
		// also try the user's own Applications folder and the
		// Applications folder (or root) of every other mounted volume.
		// Wine prefixes (CrossOver, Whisky) are searched below as on
		// Linux.
		add("/")
		add("/Applications")
		if home, err := os.UserHomeDir(); err == nil {
			add(filepath.Join(home, "Applications"))
		}
		volumes, _ := os.ReadDir("/Volumes")
		for _, v := range volumes { // ReadDir sorts by name
			if strings.HasPrefix(v.Name(), ".") {
				continue
			}
			add(filepath.Join("/Volumes", v.Name(), "Applications"))
			add(filepath.Join("/Volumes", v.Name()))
		}
	}
	// WSL mounts Windows drives as /mnt/c, /mnt/d, ...
	if entries, err := os.ReadDir("/mnt"); err == nil {
		for _, e := range entries { // ReadDir sorts by name
			if len(e.Name()) == 1 {
				add(filepath.Join("/mnt", e.Name()))
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return roots
	}
	for _, pattern := range []string{
		"Games/*",
		".wine",
		".local/share/lutris/prefixes/*",
		".var/app/net.lutris.Lutris/data/lutris/prefixes/*",
		".steam/steam/steamapps/compatdata/*/pfx",
		".local/share/Steam/steamapps/compatdata/*/pfx",
	} {
		prefixes, _ := filepath.Glob(filepath.Join(home, pattern))
		sort.Strings(prefixes)
		for _, prefix := range prefixes {
			add(filepath.Join(prefix, "drive_c"))
		}
	}
	return roots
}

// InWowDir returns the DOINK.lua paths under one WoW install folder (the one
// holding _forever_ etc.). They may not exist yet, before the first /reload.
func InWowDir(wow string) []string {
	if !isDir(wow) {
		return nil
	}
	var found []string
	flavors, _ := filepath.Glob(filepath.Join(wow, "_*_"))
	sort.Strings(flavors)
	for _, flavor := range flavors {
		if _, err := os.Stat(filepath.Join(flavor, "Interface", "AddOns", "DOINK")); err != nil {
			continue
		}
		dirs, _ := filepath.Glob(filepath.Join(flavor, "WTF", "Account", "*", "SavedVariables"))
		sort.Strings(dirs)
		for _, dir := range dirs {
			found = append(found, filepath.Join(dir, "DOINK.lua"))
		}
	}
	return found
}

// SavedVariables searches every root for the usual install folders.
func SavedVariables(roots []string) []string {
	var found []string
	for _, root := range roots {
		for _, rel := range WowDirs {
			found = append(found, InWowDir(filepath.Join(root, filepath.FromSlash(rel)))...)
		}
	}
	return found
}
