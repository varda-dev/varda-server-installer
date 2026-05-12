package serverinstaller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type ModSpec struct {
	FileName string
	URL      string
}

func ReconcileMods(targetDir string, mods []ManifestMod, force bool, workerCount int) error {
	if workerCount < 1 || workerCount > 16 {
		return fmt.Errorf("download worker count must be between 1 and 16")
	}
	if len(mods) == 0 {
		return fmt.Errorf("manifest does not contain any server mod jars")
	}

	modsPath := modsDir(targetDir)
	if err := os.MkdirAll(modsPath, 0o755); err != nil {
		return err
	}

	specs := make([]ModSpec, 0, len(mods))
	for _, mod := range mods {
		if !isSafeModFilename(mod.Filename) {
			return fmt.Errorf("unsafe or non-jar mod filename in manifest: %s", mod.Filename)
		}
		if err := validateURLScheme(mod.URL, "manifest mod url", "http", "https", "file"); err != nil {
			return fmt.Errorf("%s for %s", err, mod.Filename)
		}
		specs = append(specs, ModSpec{FileName: mod.Filename, URL: mod.URL})
	}

	sort.Slice(specs, func(i, j int) bool {
		if strings.EqualFold(specs[i].FileName, specs[j].FileName) {
			return specs[i].URL < specs[j].URL
		}
		return strings.ToLower(specs[i].FileName) < strings.ToLower(specs[j].FileName)
	})

	desired := make(map[string]ModSpec, len(specs))
	var downloads []ModSpec
	for _, mod := range specs {
		desired[strings.ToLower(mod.FileName)] = mod
		target := filepath.Join(modsPath, mod.FileName)
		if !force {
			info, err := os.Stat(target)
			if err == nil {
				if info.IsDir() {
					return fmt.Errorf("target mod path is a directory: %s", target)
				}
				if info.Size() > 0 {
					fmt.Printf("Keeping current mod: %s\n", mod.FileName)
					continue
				}
			}
			if err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		downloads = append(downloads, mod)
	}

	if err := downloadMods(targetDir, downloads, force, workerCount); err != nil {
		return err
	}

	entries, err := os.ReadDir(modsPath)
	if err != nil {
		return err
	}

	known := make(map[string]struct{}, len(desired))
	for key := range desired {
		known[key] = struct{}{}
	}

	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(modsPath, name)
		if _, ok := known[strings.ToLower(name)]; ok {
			continue
		}
		if entry.IsDir() {
			fmt.Printf("Removing unmanaged directory from mods/: %s\n", name)
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			continue
		}

		fmt.Printf("Removing unmanaged file from mods/: %s\n", name)
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	return nil
}

func downloadMods(targetDir string, mods []ModSpec, force bool, workerCount int) error {
	if len(mods) == 0 {
		return nil
	}

	modsPath := modsDir(targetDir)
	jobs := make(chan ModSpec)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	worker := func() {
		defer wg.Done()
		for mod := range jobs {
			target := filepath.Join(modsPath, mod.FileName)
			if err := downloadToFile(mod.URL, target, force, mod.FileName); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", mod.FileName, err))
				mu.Unlock()
			}
		}
	}

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	for _, mod := range mods {
		jobs <- mod
	}
	close(jobs)
	wg.Wait()

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
