package sudoku

import (
	"context"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
)

type Status struct {
	Installed       bool   `json:"installed"`
	Version         string `json:"version"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

func GetStatus(ctx context.Context) (Status, error) {
	binDir := config.GetBinFolderPath()
	status := Status{
		Installed: IsInstalled(binDir),
		Version:   readInstalledVersion(binDir),
	}
	latest, latestErr := LatestRelease(ctx)
	if latestErr != nil {
		return status, nil
	}
	status.LatestVersion = latest
	if status.Installed && latest != "" {
		status.UpdateAvailable = status.Version == "" || normalizeReleaseVersion(status.Version) != normalizeReleaseVersion(latest)
	}
	return status, nil
}

func Update(ctx context.Context) error {
	_, err := InstallLatest(ctx, config.GetBinFolderPath())
	return err
}

func normalizeReleaseVersion(value string) string {
	value = strings.TrimSpace(value)
	return strings.TrimPrefix(value, "v")
}
