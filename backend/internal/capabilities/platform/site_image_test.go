package platform

import "testing"

func TestSiteImageSupportsExternalURLAndLocalUploadedReference(t *testing.T) {
	for _, key := range []string{"site_logo", "site_logo_dark", "site_favicon"} {
		for _, value := range []string{"", "https://cdn.example/logo.svg", "/media/11111111-2222-3333-4444-555555555555"} {
			if err := ValidateSettingValue(key, value); err != nil {
				t.Fatal(key, value, err)
			}
		}
		for _, value := range []string{"/private/file", "//remote.example/image", "javascript:alert(1)", "/media/../private", "/media/11111111-2222-3333-4444-555555555555?x=1"} {
			if err := ValidateSettingValue(key, value); err == nil {
				t.Fatal("invalid asset accepted", key, value)
			}
		}
	}
	if err := ValidateSettingValue("site_url", "/media/11111111-2222-3333-4444-555555555555"); err == nil {
		t.Fatal("ordinary URLs accept local assets")
	}
}
