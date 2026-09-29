package service

// IsEnabled reports whether the Telemt systemd service is enabled without
// performing the heavier status/version checks used by Status().
func (TelemtService) IsEnabled() bool {
	return systemctl("is-enabled", "--quiet", telemtServiceName) == nil
}
