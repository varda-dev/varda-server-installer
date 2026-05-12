package serverinstaller

import (
	"fmt"
	"os"
)

func WriteJvmArgs(targetDir string, force bool) error {
	path := targetPath(targetDir, "user_jvm_args.txt")
	if !force {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fmt.Printf("Keeping current JVM args: %s\n", path)
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	const content = "# JVM memory settings for Varda.\n" +
		"# Adjust these based on available server RAM.\n" +
		"-Xms4G\n" +
		"-Xmx6G\n"

	return os.WriteFile(path, []byte(content), 0o644)
}
