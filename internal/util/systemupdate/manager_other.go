//go:build !linux

package systemupdate

import (
	"context"
	"fmt"
)

func GetStatus(ctx context.Context, allPackages bool) (Status, error) {
	return Status{}, fmt.Errorf("system package management is only supported on Linux")
}

func Refresh(ctx context.Context, allPackages bool) (Status, error) {
	return Status{}, fmt.Errorf("system package management is only supported on Linux")
}

func Apply(ctx context.Context, allPackages bool) (UpdateResult, error) {
	return UpdateResult{}, fmt.Errorf("system package management is only supported on Linux")
}

func Reboot(ctx context.Context) error {
	return fmt.Errorf("system reboot is only supported on Linux")
}
