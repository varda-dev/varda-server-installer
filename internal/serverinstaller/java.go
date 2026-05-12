package serverinstaller

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

var javaVersionPattern = regexp.MustCompile(`version "([^"]+)"`)

func RequireJava21() error {
	if _, err := exec.LookPath("java"); err != nil {
		return fmt.Errorf("java was not found; Java 21+ is required")
	}

	out, err := exec.Command("java", "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("java -version failed: %w\n%s", err, bytes.TrimSpace(out))
	}

	version, err := ParseJavaVersion(string(out))
	if err != nil {
		return err
	}
	if version < 21 {
		return fmt.Errorf("Java 21+ is required; found Java %d", version)
	}

	return nil
}

func ParseJavaVersion(output string) (int, error) {
	match := javaVersionPattern.FindStringSubmatch(output)
	if len(match) < 2 {
		return 0, fmt.Errorf("could not parse Java version")
	}

	parts := bytes.Split([]byte(match[1]), []byte("."))
	var majorText string
	if len(parts) > 0 && bytes.Equal(parts[0], []byte("1")) && len(parts) > 1 {
		majorText = string(parts[1])
	} else {
		majorText = string(parts[0])
	}

	major, err := strconv.Atoi(majorText)
	if err != nil {
		return 0, fmt.Errorf("could not parse Java version: %s", match[1])
	}

	return major, nil
}
