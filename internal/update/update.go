// Package update implements self-update from GitHub releases.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Repo is the GitHub repository releases are fetched from.
const Repo = "EmreErdogan/mds"

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

var client = &http.Client{Timeout: 60 * time.Second}

// Run checks for a newer release and replaces the running binary with it.
func Run(current string, args []string) error {
	fs := flag.NewFlagSet("mds update", flag.ExitOnError)
	force := fs.Bool("force", false, "reinstall even if already up to date")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: mds update [--force]")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	rel, err := latest()
	if err != nil {
		return fmt.Errorf("checking latest release: %w", err)
	}
	latestVer := strings.TrimPrefix(rel.TagName, "v")
	curVer := strings.TrimPrefix(current, "v")
	if !*force && curVer == latestVer {
		fmt.Printf("mds %s is already the latest version\n", current)
		return nil
	}

	assetName := AssetName(runtime.GOOS, runtime.GOARCH)
	var assetURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case assetName:
			assetURL = a.URL
		case "checksums.txt":
			sumsURL = a.URL
		}
	}
	if assetURL == "" {
		return fmt.Errorf("release %s has no binary for %s/%s", rel.TagName, runtime.GOOS, runtime.GOARCH)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	fmt.Printf("downloading mds %s (%s)...\n", rel.TagName, assetName)
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".mds-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", filepath.Dir(exe), err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	sum, err := download(assetURL, tmp)
	tmp.Close()
	if err != nil {
		return err
	}

	if sumsURL != "" {
		want, err := expectedSum(sumsURL, assetName)
		if err != nil {
			return err
		}
		if want != "" && want != sum {
			return fmt.Errorf("checksum mismatch for %s", assetName)
		}
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("cannot replace %s: %w", exe, err)
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		_ = os.Rename(old, exe)
		return fmt.Errorf("cannot install new binary: %w", err)
	}
	_ = os.Remove(old)

	fmt.Printf("updated mds %s -> %s\n", current, rel.TagName)
	return nil
}

// AssetName returns the release asset name for an OS/arch pair.
func AssetName(goos, goarch string) string {
	name := "mds_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func latest() (*release, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func download(url string, dst io.Writer) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func expectedSum(url, name string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", nil
}
