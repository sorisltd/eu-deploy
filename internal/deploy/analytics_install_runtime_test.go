package deploy

import (
	"strings"
	"testing"
)

func TestRenderGeoIPUpdateConfigTemplate(t *testing.T) {
	conf := renderGeoIPUpdateConfigTemplate()

	if !strings.Contains(conf, "AccountID 1316096") {
		t.Fatalf("expected GeoIP config template to include the MaxMind account ID, got:\n%s", conf)
	}

	if !strings.Contains(conf, "EditionIDs GeoLite2-ASN GeoLite2-City GeoLite2-Country") {
		t.Fatalf("expected GeoIP config template to include the required editions, got:\n%s", conf)
	}
}

func TestRenderAnalyticsRefreshScriptSupportsGeoIPUpdate(t *testing.T) {
	script := renderAnalyticsRefreshScript("/opt/eu-deploy/analytics/maxmind", "/opt/eu-deploy/analytics/maxmind/maxmind.env", "/opt/eu-deploy/analytics/maxmind/GeoIP.conf")

	if !strings.Contains(script, "command -v geoipupdate") {
		t.Fatalf("expected refresh script to support geoipupdate, got:\n%s", script)
	}

	if !strings.Contains(script, "geoip_conf='/opt/eu-deploy/analytics/maxmind/GeoIP.conf'") {
		t.Fatalf("expected refresh script to point at GeoIP.conf, got:\n%s", script)
	}
}
