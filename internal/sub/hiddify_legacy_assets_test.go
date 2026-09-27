package sub

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

func TestLegacyHiddifyBrowserAssetsStayUnderImportedAlias(t *testing.T) {
	const subID = "b1337b29-8d60-4491-a468-c2bf120cb878"

	for _, importedPath := range []string{"ImportedRouteAlpha123", "Different_Route-456"} {
		t.Run(importedPath, func(t *testing.T) {
			aliases := []service.HiddifyLegacySubscriptionAlias{{Path: importedPath}}
			requestPath := "/" + importedPath + "/" + subID + "/"

			gotID, ok := legacyHiddifySubID(requestPath, aliases)
			if !ok || gotID != subID {
				t.Fatalf("legacy subscription route = %q, %v", gotID, ok)
			}

			if got := legacyHiddifyBasePath(requestPath, subID); got != "/"+importedPath+"/" {
				t.Fatalf("legacy SPA base path = %q, want /%s/", got, importedPath)
			}

			assetRequest := "/" + importedPath + "/assets/subpage-example.js"
			assetPath, ok := legacyHiddifyAssetPath(assetRequest, aliases)
			if !ok || assetPath != "subpage-example.js" {
				t.Fatalf("legacy asset route = %q, %v", assetPath, ok)
			}
		})
	}
}

func TestLegacyHiddifyAssetPathRejectsTraversalAndOtherAliases(t *testing.T) {
	aliases := []service.HiddifyLegacySubscriptionAlias{{Path: "ImportedRouteAlpha123"}}
	for _, requestPath := range []string{
		"/ImportedRouteAlpha123/assets/../secret",
		"/ImportedRouteAlpha123/assets/",
		"/OtherRoute/assets/subpage.js",
	} {
		if assetPath, ok := legacyHiddifyAssetPath(requestPath, aliases); ok {
			t.Fatalf("unsafe or unrelated asset path accepted: %q -> %q", requestPath, assetPath)
		}
	}
}
