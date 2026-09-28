package sub

import "net/http"

// IsLegacySubscriptionRequest reports whether r targets a validated migrated
// Hiddify subscription URL (or one of its static assets). The same predicate is
// used by the panel's domain validator so old public :443 URLs can continue to
// reach the subscription handler after the dedicated subscription port changes.
func (s *Server) IsLegacySubscriptionRequest(r *http.Request) bool {
	if s == nil || r == nil || r.URL == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	aliases, err := s.settingService.GetHiddifyLegacySubscriptionAliases()
	if err != nil {
		return false
	}
	if subID, ok := legacyHiddifySubID(r.URL.Path, aliases); ok && s.settingService.IsHiddifySubscriptionPath(subID, r.URL.Path) {
		return true
	}
	_, ok := legacyHiddifyAssetPath(r.URL.Path, aliases)
	return ok
}
