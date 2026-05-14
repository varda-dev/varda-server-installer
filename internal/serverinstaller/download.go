package serverinstaller

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxManifestSize = 5 << 20

var httpClient = &http.Client{
	Timeout: 10 * time.Minute,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	},
}

var statFile = os.Stat
var renameFile = os.Rename
var removeFile = os.Remove

type DownloadChecks struct {
	SHA1 string
	Size int64
}

func downloadURLToBytes(rawURL string) ([]byte, error) {
	source, err := openDownload(rawURL)
	if err != nil {
		return nil, err
	}
	defer source.Close()

	limited := io.LimitReader(source, maxManifestSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > maxManifestSize {
		return nil, fmt.Errorf("manifest exceeds maximum size of %d bytes", maxManifestSize)
	}
	return data, nil
}

func downloadToFile(rawURL, targetPath string, force bool, label string, checks ...DownloadChecks) error {
	var check DownloadChecks
	if len(checks) > 0 {
		check = checks[0]
	}

	if !force {
		info, err := statFile(targetPath)
		if err == nil {
			if info.IsDir() {
				return fmt.Errorf("target path is a directory: %s", targetPath)
			}
			if info.Size() > 0 {
				if check.Size > 0 && info.Size() != check.Size {
					goto download
				}
				if check.SHA1 != "" {
					actual, err := sha1File(targetPath)
					if err != nil {
						return err
					}
					if strings.EqualFold(actual, check.SHA1) {
						fmt.Printf("Keeping current %s: %s\n", label, targetPath)
						return nil
					}
					goto download
				}
				fmt.Printf("Keeping current %s: %s\n", label, targetPath)
				return nil
			}
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

download:
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(targetPath), filepath.Base(targetPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	fmt.Printf("Downloading %s...\n", label)

	if err := writeDownloadedContent(tmp, rawURL); err != nil {
		tmp.Close()
		return err
	}
	if check.Size > 0 {
		info, err := tmp.Stat()
		if err != nil {
			tmp.Close()
			return err
		}
		if info.Size() != check.Size {
			tmp.Close()
			return fmt.Errorf("%s size mismatch: got %d want %d", label, info.Size(), check.Size)
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if check.SHA1 != "" {
		if err := verifyFileSHA1(tmpName, check.SHA1, label); err != nil {
			return err
		}
	}

	return replaceFile(tmpName, targetPath)
}

func replaceFile(tempPath, targetPath string) error {
	backupPath := targetPath + ".bak"

	if _, err := statFile(targetPath); err == nil {
		_ = removeFile(backupPath)

		if err := renameFile(targetPath, backupPath); err != nil {
			return err
		}

		if err := renameFile(tempPath, targetPath); err != nil {
			restoreErr := renameFile(backupPath, targetPath)
			if restoreErr != nil {
				return fmt.Errorf("%w; restore failed: %v", err, restoreErr)
			}
			return err
		}

		_ = removeFile(backupPath)
		return nil
	} else if os.IsNotExist(err) {
		if err := renameFile(tempPath, targetPath); err != nil {
			return err
		}
		return nil
	} else {
		return err
	}
}

func writeDownloadedContent(dest *os.File, rawURL string) error {
	source, err := openDownload(rawURL)
	if err != nil {
		return err
	}
	defer source.Close()

	_, err = io.Copy(dest, source)
	return err
}

func openDownload(rawURL string) (io.ReadCloser, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	switch parsed.Scheme {
	case "file":
		sourcePath := parsed.Path
		if runtime.GOOS == "windows" && strings.HasPrefix(sourcePath, "/") && len(sourcePath) > 2 && sourcePath[2] == ':' {
			sourcePath = sourcePath[1:]
		}
		sourcePath = filepath.FromSlash(sourcePath)
		if sourcePath == "" {
			return nil, fmt.Errorf("file URL has no path: %s", rawURL)
		}
		return os.Open(sourcePath)

	case "http", "https":
		resp, err := httpClient.Get(rawURL)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			return nil, fmt.Errorf("download failed: HTTP %d %s\n%s", resp.StatusCode, resp.Status, strings.TrimSpace(string(body)))
		}
		return resp.Body, nil

	default:
		return nil, fmt.Errorf("unsupported URL scheme: %s", parsed.Scheme)
	}
}
