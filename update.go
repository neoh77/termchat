package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releaseURL = "https://api.github.com/repos/neoh77/termchat/releases/latest"
const maxBinarySize = 100 << 20

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// compareVersions compares the stable SemVer tags used by the release workflow.
func compareVersions(a, b string) (int, error) {
	parse := func(tag string) ([3]int, error) {
		var parts [3]int
		if !strings.HasPrefix(tag, "v") {
			return parts, fmt.Errorf("invalid release version %q", tag)
		}
		fields := strings.Split(tag[1:], ".")
		if len(fields) != 3 {
			return parts, fmt.Errorf("invalid release version %q", tag)
		}
		for i, field := range fields {
			value, err := strconv.Atoi(field)
			if err != nil || value < 0 || strconv.Itoa(value) != field {
				return parts, fmt.Errorf("invalid release version %q", tag)
			}
			parts[i] = value
		}
		return parts, nil
	}
	left, err := parse(a)
	if err != nil {
		return 0, err
	}
	right, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range left {
		if left[i] < right[i] {
			return -1, nil
		}
		if left[i] > right[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func getRelease(ctx context.Context, client *http.Client, url string) (release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "termchat")
	response, err := client.Do(request)
	if err != nil {
		return release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("GitHub release check: %s", response.Status)
	}
	var latest release
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&latest); err != nil {
		return release{}, fmt.Errorf("read GitHub release: %w", err)
	}
	if _, err := compareVersions(latest.TagName, latest.TagName); err != nil {
		return release{}, err
	}
	return latest, nil
}

// updateLatest replaces the installed binary only after a complete download.
// Development builds have no release version, so an explicit update installs the latest release.
func updateLatest(ctx context.Context, client *http.Client, apiURL, executable, current string) (string, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return "", errors.New("updates are available only for Linux amd64")
	}
	latest, err := getRelease(ctx, client, apiURL)
	if err != nil {
		return "", err
	}
	if current != "dev" {
		comparison, err := compareVersions(current, latest.TagName)
		if err != nil {
			return "", err
		}
		if comparison >= 0 {
			return "Already up to date (" + current + ")", nil
		}
	}
	assetURL := ""
	for _, asset := range latest.Assets {
		if asset.Name == "termchat" {
			assetURL = asset.URL
			break
		}
	}
	if assetURL == "" {
		return "", errors.New("latest release has no termchat binary")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "termchat")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download update: %s", response.Status)
	}
	target, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".termchat-update-*")
	if err != nil {
		return "", fmt.Errorf("stage update: %w", err)
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	size, err := io.Copy(temp, io.LimitReader(response.Body, maxBinarySize+1))
	if err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	if size == 0 || size > maxBinarySize {
		return "", errors.New("downloaded binary is empty or too large")
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	header := make([]byte, 20)
	if _, err := io.ReadFull(temp, header); err != nil || string(header[:4]) != "\x7fELF" || header[4] != 2 || header[5] != 1 || header[18] != 0x3e || header[19] != 0 {
		return "", errors.New("downloaded file is not a Linux amd64 executable")
	}
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temp.Name(), target); err != nil {
		return "", fmt.Errorf("install update: %w", err)
	}
	return "Updated to " + latest.TagName + "; restart termchat to use it", nil
}

func updateInstalled() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return updateLatest(ctx, &http.Client{Timeout: 60 * time.Second}, releaseURL, executable, version)
}
