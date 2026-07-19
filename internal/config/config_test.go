package config

import "testing"

func TestValidateRoutesRedirectDefaultsAndHTTPS(t *testing.T) {
	valid := []RouteSpec{{Hostnames: []string{"safebuild.lt", "www.safebuild.lt"}, Redirect: "https://bustora.lt/meistrams"}}
	if err := ValidateRoutes(valid); err != nil {
		t.Fatalf("valid redirect rejected: %v", err)
	}

	invalid := []RouteSpec{{Hostname: "safebuild.lt", Redirect: "http://bustora.lt/meistrams"}}
	if err := ValidateRoutes(invalid); err == nil {
		t.Fatal("expected non-https redirect to fail")
	}
}

func TestValidateRoutesLegacyAndPreservePrefix(t *testing.T) {
	routes := []RouteSpec{
		{Hostname: "legacy.example.com", Path: "/book", Target: "web"},
		{Hostname: "bustora.lt", Path: "/darbai", Target: "web", PreservePrefix: true},
	}
	if err := ValidateRoutes(routes); err != nil {
		t.Fatalf("proxy routes rejected: %v", err)
	}
}
