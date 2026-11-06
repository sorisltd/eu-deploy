package analytics

import "testing"

func TestIsKnownBotUserAgent(t *testing.T) {
	testCases := []struct {
		name      string
		userAgent string
		want      bool
	}{
		{
			name:      "googlebot",
			userAgent: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			want:      true,
		},
		{
			name:      "openai crawler",
			userAgent: "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2; +https://openai.com/gptbot)",
			want:      true,
		},
		{
			name:      "script client",
			userAgent: "python-requests/2.32.3",
			want:      true,
		},
		{
			name:      "headless browser",
			userAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/146.0.0.0 Safari/537.36",
			want:      true,
		},
		{
			name:      "instagram in app browser",
			userAgent: "Mozilla/5.0 (Linux; Android 16; SM-S711B Build/BP2A.250605.031.A3; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/146.0.7680.141 Mobile Safari/537.36 Instagram 420.0.0.55.74 Android",
			want:      false,
		},
		{
			name:      "empty user agent",
			userAgent: "",
			want:      true,
		},
		{
			name:      "whatsapp link preview",
			userAgent: "WhatsApp/2.23.20.0 A",
			want:      true,
		},
		{
			name:      "google page renderer",
			userAgent: "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Mobile Safari/537.36 (compatible; Google-PageRenderer; +https://developers.google.com/speed/pagespeed/insights)",
			want:      true,
		},
		{
			name:      "uptime kuma",
			userAgent: "Uptime-Kuma/1.23.16",
			want:      true,
		},
		{
			name:      "site24x7 monitor",
			userAgent: "Mozilla/5.0 (compatible; Site24x7)",
			want:      true,
		},
		{
			name:      "cloudflare health check",
			userAgent: "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/; pool-id: 0123456789abcdef)",
			want:      true,
		},
		{
			name:      "censys scanner",
			userAgent: "Mozilla/5.0 (compatible; CensysInspect/1.1; +https://about.censys.io/)",
			want:      true,
		},
		{
			name:      "zgrab scanner",
			userAgent: "Mozilla/5.0 zgrab/0.x",
			want:      true,
		},
		{
			name:      "node undici",
			userAgent: "undici",
			want:      true,
		},
		{
			name:      "python urllib",
			userAgent: "Python-urllib/3.12",
			want:      true,
		},
		{
			name:      "headless firefox",
			userAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0 Headless",
			want:      true,
		},
		{
			name:      "eu-deploy dashboard edge check",
			userAgent: "eu-deploy-dashboard edge-check-bot",
			want:      true,
		},
		{
			name:      "cubot android phone",
			userAgent: "Mozilla/5.0 (Linux; Android 13; CUBOT KINGKONG 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Mobile Safari/537.36",
			want:      false,
		},
		{
			name:      "safari ios",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Mobile/15E148 Safari/604.1",
			want:      false,
		},
		{
			name:      "firefox desktop",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:144.0) Gecko/20100101 Firefox/144.0",
			want:      false,
		},
		{
			name:      "edge desktop",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
			want:      false,
		},
		{
			name:      "facebook in app browser",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/FBIOS;FBAV/520.0.0.38.101;FBBV/700000000;FBDV/iPhone16,2;FBMD/iPhone;FBSN/iOS;FBSV/18.6;FBSS/3;FBCR/;FBID/phone;FBLC/lt_LT;FBOP/5]",
			want:      false,
		},
		{
			name:      "normal chrome",
			userAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
			want:      false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isKnownBotUserAgent(testCase.userAgent); got != testCase.want {
				t.Fatalf("isKnownBotUserAgent(%q) = %v, want %v", testCase.userAgent, got, testCase.want)
			}
		})
	}
}

func TestFirstHeaderValueIgnoresHeaderCase(t *testing.T) {
	headers := map[string][]string{
		"user-agent": {"Mozilla/5.0"},
	}

	if got := firstHeaderValue(headers, "User-Agent"); got != "Mozilla/5.0" {
		t.Fatalf("firstHeaderValue returned %q", got)
	}
}
