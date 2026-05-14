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
	Name     string
	FileName string
	URL      string
	SHA1     string
	Size     int64
}

func ReconcileMods(targetDir string, mods []ManifestMod, force bool, workerCount int) error {
	if workerCount < 1 || workerCount > 16 {
		return fmt.Errorf("download worker count must be between 1 and 16")
	}
	if len(mods) == 0 {
		return fmt.Errorf("manifest does not contain any server mod jars")
	}

	specs := make([]ModSpec, 0, len(mods))
	for _, mod := range mods {
		spec, err := modSpecFromManifestMod(mod)
		if err != nil {
			return err
		}
		specs = append(specs, spec)
	}

	modsPath := modsDir(targetDir)
	if err := os.MkdirAll(modsPath, 0o755); err != nil {
		return err
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
				if info.Size() == 0 {
					downloads = append(downloads, mod)
					continue
				}
				if mod.Size > 0 && info.Size() != mod.Size {
					downloads = append(downloads, mod)
					continue
				}
				actualSHA1, err := sha1File(target)
				if err != nil {
					return err
				}
				if strings.EqualFold(actualSHA1, mod.SHA1) {
					fmt.Printf("Keeping current mod: %s\n", modLabel(mod))
					continue
				}
			}
			if err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		downloads = append(downloads, mod)
	}

	if err := downloadMods(targetDir, downloads, workerCount); err != nil {
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

func downloadMods(targetDir string, mods []ModSpec, workerCount int) error {
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
			if err := downloadToFile(mod.URL, target, true, modLabel(mod), DownloadChecks{SHA1: mod.SHA1, Size: mod.Size}); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", modLabel(mod), err))
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

func modLabel(mod ModSpec) string {
	if mod.Name != "" {
		return mod.Name
	}
	return mod.FileName
}

func modSpecFromManifestMod(mod ManifestMod) (ModSpec, error) {
	label := strings.TrimSpace(mod.Name)
	if label == "" {
		label = strings.TrimSpace(mod.URL)
		if label == "" {
			label = "manifest mod"
		}
	}
	if strings.TrimSpace(mod.Name) == "" {
		return ModSpec{}, fmt.Errorf("%s: manifest mod name must be non-empty", label)
	}
	if strings.TrimSpace(mod.WebsiteURL) == "" {
		return ModSpec{}, fmt.Errorf("%s: manifest mod website_url must be non-empty", label)
	}
	if mod.Size <= 0 {
		return ModSpec{}, fmt.Errorf("%s: manifest mod size must be positive when present", label)
	}
	if err := validateURLScheme(mod.URL, "manifest mod url", "http", "https"); err != nil {
		return ModSpec{}, fmt.Errorf("%s: %w", label, err)
	}
	filename, err := inferFilenameFromURL(mod.URL)
	if err != nil {
		return ModSpec{}, fmt.Errorf("%s: %w", label, err)
	}
	if !isSafeModFilename(filename) {
		return ModSpec{}, fmt.Errorf("%s: unsafe or non-jar mod filename in manifest: %s", label, filename)
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".jar") {
		return ModSpec{}, fmt.Errorf("%s: manifest mod url must end with .jar: %s", label, filename)
	}
	if err := validateURLScheme(mod.WebsiteURL, "manifest mod website_url", "http", "https"); err != nil {
		return ModSpec{}, fmt.Errorf("%s: %w", label, err)
	}
	if mod.SHA1 == "" {
		return ModSpec{}, fmt.Errorf("%s: manifest mod sha1 must be non-empty", label)
	}
	if err := validateSHA1Hex(mod.SHA1, fmt.Sprintf("manifest mod sha1 for %s", label)); err != nil {
		return ModSpec{}, err
	}
	return ModSpec{Name: mod.Name, FileName: filename, URL: mod.URL, SHA1: mod.SHA1, Size: mod.Size}, nil
}
