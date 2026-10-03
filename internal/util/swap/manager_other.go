//go:build !linux

package swap

import "context"

func GetStatus() (Status, error)                   { return Status{}, ErrUnsupported }
func Summary() (uint64, uint64, error)             { return 0, 0, ErrUnsupported }
func GetRecommendations() (Recommendation, error)  { return Recommendation{}, ErrUnsupported }
func GetZramInstallInfo() (ZramInstallInfo, error) { return ZramInstallInfo{}, ErrUnsupported }
func InstallZram(ctx context.Context) error        { return ErrUnsupported }
func GetConfig() (Config, error)                   { return Config{}, ErrUnsupported }
func CreateSwap(sizeMiB, priority int) error       { return ErrUnsupported }
func DeleteSwap() error                            { return ErrUnsupported }
func CreateZram(sizeMiB int, algorithm string, streams int, memoryLimitMiB, priority int) error {
	return ErrUnsupported
}
func DeleteZram() error                { return ErrUnsupported }
func SetSwappiness(value int) error    { return ErrUnsupported }
func Ensure(ctx context.Context) error { return ErrUnsupported }
