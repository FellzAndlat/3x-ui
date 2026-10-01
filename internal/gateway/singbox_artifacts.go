package gateway

// hasSingBoxGatewayArtifacts reports whether the sing-box template still
// contains any object owned by Gateway Mode. Partial state is intentionally
// treated as enabled so the UI offers Disable as a recovery path instead of
// getting stuck between a failing Enable and a failing Disable.
func hasSingBoxGatewayArtifacts(cfg map[string]any) bool {
	return hasSingBoxGatewayInbound(cfg) || hasSingBoxGatewaySniffRule(cfg)
}
