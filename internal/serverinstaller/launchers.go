package serverinstaller

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
)

func PatchLaunchers(targetDir, version string) error {
	if err := patchRunSh(targetDir, version); err != nil {
		return err
	}
	if err := patchRunBat(targetDir, version); err != nil {
		return err
	}
	return nil
}

func patchRunSh(targetDir, version string) error {
	runSh := targetPath(targetDir, "run.sh")
	expected := fmt.Sprintf("java @user_jvm_args.txt @libraries/net/neoforged/neoforge/%s/unix_args.txt nogui \"$@\"", version)
	existingExpected := fmt.Sprintf("java @user_jvm_args.txt @libraries/net/neoforged/neoforge/%s/unix_args.txt \"$@\"", version)

	content, err := os.ReadFile(runSh)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(runSh, []byte("#!/usr/bin/env sh\n"+expected+"\n"), 0o755); err != nil {
				return err
			}
			return ensureExecutable(runSh)
		}
		return err
	}

	text := string(content)
	switch {
	case strings.Contains(text, expected):
		return ensureExecutable(runSh)
	case strings.Contains(text, existingExpected):
		text = strings.ReplaceAll(text, existingExpected, expected)
	default:
		return fmt.Errorf("run.sh did not contain expected NeoForge launch command")
	}

	if err := os.WriteFile(runSh, []byte(text), 0o755); err != nil {
		return err
	}
	return ensureExecutable(runSh)
}

func patchRunBat(targetDir, version string) error {
	runBat := targetPath(targetDir, "run.bat")
	patchedLine := fmt.Sprintf("java @user_jvm_args.txt @libraries/net/neoforged/neoforge/%s/win_args.txt nogui %%*", version)
	launchLinePattern := regexp.MustCompile(
		fmt.Sprintf(
			`(?m)^java @user_jvm_args\.txt @libraries[\\/]net[\\/]neoforged[\\/]neoforge[\\/]%s[\\/]win_args\.txt(?: nogui)? %%\*\r?$`,
			regexp.QuoteMeta(version),
		),
	)

	content, err := os.ReadFile(runBat)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(
				runBat,
				[]byte("@echo off\r\n"+patchedLine+"\r\npause\r\n"),
				0o644,
			)
		}
		return err
	}

	text := string(content)
	if !launchLinePattern.MatchString(text) {
		return fmt.Errorf("run.bat did not contain expected NeoForge launch command")
	}

	text = launchLinePattern.ReplaceAllString(text, patchedLine)
	return os.WriteFile(runBat, []byte(text), 0o644)
}

func ensureExecutable(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return os.Chmod(path, 0o755)
}
