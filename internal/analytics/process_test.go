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
