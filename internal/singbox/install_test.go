package singbox

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

func TestReleaseVersionsIncludesPrereleases(t *testing.T) {
	releases := []releaseListInfo{
		{TagName: "v1.12.0", Prerelease: false},
		{TagName: "v1.13.0-beta.1", Prerelease: true},
		{TagName: "", Prerelease: true},
	}

	want := []ReleaseVersion{
		{Version: "v1.12.0", Prerelease: false},
		{Version: "v1.13.0-beta.1", Prerelease: true},
	}
	if got := releaseVersions(releases); !reflect.DeepEqual(got, want) {
		t.Fatalf("versions = %#v, want %#v", got, want)
	}
}

func TestIsPreReleaseVersion(t *testing.T) {
	stable := []string{"v1.12.0", "v1.13.0"}
	for _, version := range stable {
		if isPreReleaseVersion(version) {
			t.Fatalf("isPreReleaseVersion(%q) = true", version)
		}
	}

	unstable := []string{
		"v1.13.0-alpha.1",
		"v1.13.0-beta.2",
		"v1.13.0-rc.1",
		"v1.13.0-pre",
		"v1.13.0-dev",
		"v1.13.0-nightly.20260924",
	}
	for _, version := range unstable {
		if !isPreReleaseVersion(version) {
			t.Fatalf("isPreReleaseVersion(%q) = false", version)
		}
	}
}

func TestReleaseAtomFeedVersions(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <title>1.14.1</title>
    <link href="https://github.com/SagerNet/sing-box/releases/tag/v1.14.1"/>
  </entry>
  <entry>
    <title>1.15.0-alpha.6</title>
    <link href="https://github.com/SagerNet/sing-box/releases/tag/v1.15.0-alpha.6"/>
  </entry>
</feed>`
	var parsed releaseAtomFeed
	if err := xml.Unmarshal([]byte(feed), &parsed); err != nil {
		t.Fatalf("xml.Unmarshal() error = %v", err)
	}
	var releases []releaseListInfo
	for _, entry := range parsed.Entries {
		tag := entry.Title
		for _, link := range entry.Links {
			const marker = "/releases/tag/"
			if idx := strings.Index(link.Href, marker); idx >= 0 {
				tag = strings.Trim(link.Href[idx+len(marker):], "/")
				break
			}
		}
		if tag != "" {
			releases = append(releases, releaseListInfo{TagName: tag, Prerelease: isPreReleaseVersion(tag)})
		}
	}
	want := []ReleaseVersion{
		{Version: "v1.14.1", Prerelease: false},
		{Version: "v1.15.0-alpha.6", Prerelease: true},
	}
	if got := releaseVersions(releases); !reflect.DeepEqual(got, want) {
		t.Fatalf("versions = %#v, want %#v", got, want)
	}
}
