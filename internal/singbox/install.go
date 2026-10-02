package singbox

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	releaseAPI  = "https://api.github.com/repos/SagerNet/sing-box/releases/latest"
	releasesAPI = "https://api.github.com/repos/SagerNet/sing-box/releases"
)

type releaseInfo struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}
type releaseListInfo struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
}

type ReleaseVersion struct {
	Version    string `json:"version"`
	Prerelease bool   `json:"prerelease"`
}

type extractedRelease struct {
	Binary       string
	RuntimeFiles []string
}

func releaseVersions(releases []releaseListInfo) []ReleaseVersion {
	versions := make([]ReleaseVersion, 0, len(releases))
	for _, release := range releases {
		if release.TagName != "" {
			versions = append(versions, ReleaseVersion{
				Version:    release.TagName,
				Prerelease: release.Prerelease || isPreReleaseVersion(release.TagName),
			})
		}
	}
	return versions
}

func isPreReleaseVersion(version string) bool {
	lower := strings.ToLower(version)
	for _, marker := range []string{"-alpha", "-beta", "-rc", "-pre", "-dev", "-nightly"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func InstallLatest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch sing-box release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sing-box release API returned %s", resp.Status)
	}
	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return installRelease(ctx, rel)
}

func ListVersions(ctx context.Context) ([]ReleaseVersion, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI+"?per_page=30", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var releases []releaseListInfo
			if err := json.NewDecoder(resp.Body).Decode(&releases); err == nil {
				return releaseVersions(releases), nil
			}
		}
	}

	// GitHub's unauthenticated REST API is rate-limited. The public releases
	// Atom feed is served separately and provides the tag list we need for the
	// version picker, including prereleases.
	versions, feedErr := listVersionsFromAtom(ctx)
	if feedErr == nil {
		return versions, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch sing-box releases: API: %w; Atom fallback: %w", err, feedErr)
	}
	return nil, fmt.Errorf("sing-box releases API returned %s; Atom fallback failed: %w", resp.Status, feedErr)
}

type releaseAtomFeed struct {
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func listVersionsFromAtom(ctx context.Context) ([]ReleaseVersion, error) {
	const feedURL = "https://github.com/SagerNet/sing-box/releases.atom"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/atom+xml, application/xml;q=0.9, text/xml;q=0.8")
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch sing-box releases atom feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sing-box releases atom feed returned %s", resp.Status)
	}
	var feed releaseAtomFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode sing-box releases atom feed: %w", err)
	}

	releases := make([]releaseListInfo, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		tag := strings.TrimSpace(entry.Title)
		for _, link := range entry.Links {
			const marker = "/releases/tag/"
			if idx := strings.Index(link.Href, marker); idx >= 0 {
				tag = strings.Trim(strings.TrimSpace(link.Href[idx+len(marker):]), "/")
				break
			}
		}
		if tag != "" {
			releases = append(releases, releaseListInfo{
				TagName:    tag,
				Prerelease: isPreReleaseVersion(tag),
			})
		}
	}
	if len(releases) == 0 {
		return nil, errors.New("sing-box releases atom feed contains no releases")
	}
	return releaseVersions(releases), nil
}

func InstallVersion(ctx context.Context, version string) (string, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return "", fmt.Errorf("sing-box version is empty")
	}
	url := "https://api.github.com/repos/SagerNet/sing-box/releases/tags/v" + version
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch sing-box release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sing-box release API returned %s", resp.Status)
	}
	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return installRelease(ctx, rel)
}

func installRelease(ctx context.Context, rel releaseInfo) (string, error) {
	asset, sums, err := selectAssets(ctx, rel)
	if err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp("", "3x-ui-singbox-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	archivePath := filepath.Join(tmpDir, asset.Name)
	if err := download(ctx, asset.URL, archivePath); err != nil {
		return "", err
	}
	if expected := sums[asset.Name]; expected != "" {
		if err := verifySHA256(archivePath, expected); err != nil {
			return "", err
		}
	}
	extracted, err := extractReleaseFiles(archivePath, tmpDir)
	if err != nil {
		return "", err
	}
	dest := GetBinaryPath()
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	for _, runtimeFile := range extracted.RuntimeFiles {
		runtimeDest := filepath.Join(destDir, filepath.Base(runtimeFile))
		stagedRuntime := runtimeDest + ".new"
		if err := copyFile(runtimeFile, stagedRuntime); err != nil {
			return "", err
		}
		if err := os.Rename(stagedRuntime, runtimeDest); err != nil {
			_ = os.Remove(stagedRuntime)
			return "", err
		}
	}
	staged := dest + ".new"
	if err := copyFile(extracted.Binary, staged); err != nil {
		return "", err
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	if err := os.Rename(staged, dest); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	return rel.TagName, nil
}

func Uninstall() error {
	dest := GetBinaryPath()
	paths := []string{
		dest,
		filepath.Join(filepath.Dir(dest), "libcronet.so"),
		filepath.Join(filepath.Dir(dest), "libcronet.dll"),
	}
	var firstErr error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func releaseArchiveName(rel releaseInfo, target string) string {
	version := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	if version == "" {
		return ""
	}
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("sing-box-%s-%s%s", version, target, ext)
}

func selectAssets(ctx context.Context, rel releaseInfo) (struct{ Name, URL string }, map[string]string, error) {
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	target := runtime.GOOS + "-" + arch
	var archive struct{ Name, URL string }
	sums := map[string]string{}
	canonical := strings.ToLower(releaseArchiveName(rel, target))
	if canonical != "" {
		for _, a := range rel.Assets {
			if strings.ToLower(a.Name) == canonical {
				archive.Name, archive.URL = a.Name, a.URL
				break
			}
		}
	}
	if archive.Name == "" {
		for _, a := range rel.Assets {
			n := a.Name
			l := strings.ToLower(n)
			if strings.Contains(l, "sha256") && (strings.Contains(l, "sum") || strings.Contains(l, "checksums")) {
				continue
			}
			if strings.Contains(l, strings.ToLower(target)) && (strings.HasSuffix(l, ".tar.gz") || strings.HasSuffix(l, ".zip")) {
				archive.Name, archive.URL = a.Name, a.URL
				break
			}
		}
	}
	if archive.Name == "" {
		// sing-box uses GOOS/GOARCH names in official release archives.
		for _, a := range rel.Assets {
			l := strings.ToLower(a.Name)
			if strings.Contains(l, runtime.GOOS) && strings.Contains(l, strings.ToLower(arch)) && (strings.HasSuffix(l, ".tar.gz") || strings.HasSuffix(l, ".zip")) {
				archive.Name, archive.URL = a.Name, a.URL
				break
			}
		}
	}
	if archive.Name == "" {
		return archive, sums, fmt.Errorf("no sing-box release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// The official release API may expose a separate SHA256SUMS asset. It is
	// intentionally not required: releases without one remain installable.
	for _, a := range rel.Assets {
		if strings.Contains(strings.ToLower(a.Name), "sha256") && strings.Contains(strings.ToLower(a.Name), "sum") {
			if body, err := fetchText(ctx, a.URL); err == nil {
				for _, line := range strings.Split(body, "\n") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						sums[strings.TrimPrefix(fields[1], "*")] = fields[0]
					}
				}
			}
		}
	}
	return archive, sums, nil
}

func download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func fetchText(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(expected)) {
		return fmt.Errorf("sing-box archive checksum mismatch: got %s, want %s", got, expected)
	}
	return nil
}

func archiveRuntimeFile(name string) bool {
	switch strings.ToLower(filepath.Base(name)) {
	case "libcronet.so", "libcronet.dll":
		return true
	default:
		return false
	}
}

func extractReleaseFiles(archivePath, dir string) (extractedRelease, error) {
	result := extractedRelease{}
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		z, err := zip.OpenReader(archivePath)
		if err != nil {
			return result, err
		}
		defer z.Close()
		for _, f := range z.File {
			base := filepath.Base(f.Name)
			if base != "sing-box" && base != "sing-box.exe" && !archiveRuntimeFile(base) {
				continue
			}
			dst := filepath.Join(dir, base)
			if err := extractZipFile(f, dst); err != nil {
				return result, err
			}
			if base == "sing-box" || base == "sing-box.exe" {
				result.Binary = dst
			} else {
				result.RuntimeFiles = append(result.RuntimeFiles, dst)
			}
		}
		if result.Binary == "" {
			return result, fmt.Errorf("sing-box binary not found in archive")
		}
		return result, nil
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return result, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return result, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
		base := filepath.Base(h.Name)
		if base != "sing-box" && !archiveRuntimeFile(base) {
			continue
		}
		dst := filepath.Join(dir, base)
		out, err := os.Create(dst)
		if err != nil {
			return result, err
		}
		if _, err = io.Copy(out, tr); err != nil {
			_ = out.Close()
			return result, err
		}
		if err = out.Close(); err != nil {
			return result, err
		}
		if base == "sing-box" {
			result.Binary = dst
		} else {
			result.RuntimeFiles = append(result.RuntimeFiles, dst)
		}
	}
	if result.Binary == "" {
		return result, fmt.Errorf("sing-box binary not found in archive")
	}
	return result, nil
}

func extractZipFile(f *zip.File, dst string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
