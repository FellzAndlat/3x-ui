package controller

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

// NormalizeBoundRequest keeps automatically generated subscription URI
// overrides from pinning the user page to an old subscription port. Explicit
// reverse-proxy/custom URIs are preserved: only a value that is unchanged in
// the submitted form and exactly matches the URI generated from the currently
// persisted subscription settings is cleared.
func (f *updateSettingForm) NormalizeBoundRequest(c *gin.Context) {
	if f == nil || c == nil || c.Request == nil {
		return
	}

	settingService := &service.SettingService{}
	current, err := settingService.GetAllSetting()
	if err != nil || current == nil || current.SubPort == f.SubPort {
		return
	}

	requestHost := c.Request.Host
	resetGeneratedURI := func(next *string, oldURI, path string) {
		if strings.TrimSpace(*next) != strings.TrimSpace(oldURI) {
			// The operator edited this URI in the same save; respect it.
			return
		}
		if isGeneratedSubscriptionURI(settingService, oldURI, path, requestHost) {
			// Empty means dynamic: GetDefaultSettings and BuildURLs will rebuild it
			// from the newly persisted subPort/subDomain/TLS settings.
			*next = ""
		}
	}

	resetGeneratedURI(&f.SubURI, current.SubURI, current.SubPath)
	resetGeneratedURI(&f.SubJsonURI, current.SubJsonURI, current.SubJsonPath)
	resetGeneratedURI(&f.SubClashURI, current.SubClashURI, current.SubClashPath)
}

func isGeneratedSubscriptionURI(settingService *service.SettingService, rawURI, path, requestHost string) bool {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return false
	}

	matches := func(host string) bool {
		if strings.TrimSpace(host) == "" {
			return false
		}
		expected := settingService.BuildSubURIBase(host) + path
		return normalizeSubscriptionPrefix(rawURI) == normalizeSubscriptionPrefix(expected)
	}

	// The normal case: compare against the host of the settings request.
	if matches(requestHost) {
		return true
	}

	// A generated URI may have been persisted while the panel was reached via
	// another hostname. Rebuild the old automatic value using the URI's own
	// hostname so such installations are migrated as well.
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	return matches(parsed.Hostname())
}

func normalizeSubscriptionPrefix(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}
