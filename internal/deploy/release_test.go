package deploy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildReleaseIDIncludesShortSHA(t *testing.T) {
	got := BuildReleaseID("1234567890abcdef")
	if len(got) < len("20060102-150405-1234567890ab") {
		t.Fatalf("release id too short: %q", got)
	}
}

func TestParseReleaseHistory(t *testing.T) {
	records, err := parseReleaseHistory("r1\ta\t3001\timg:r1\tsha1\t2026-03-08T10:00:00Z\nr2\tb\t3002\timg:r2\tsha2\t2026-03-08T10:05:00Z\n")
	if err != nil {
		t.Fatalf("parseReleaseHistory: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[1].ID != "r2" || records[1].Port != 3002 {
		t.Fatalf("unexpected second record: %#v", records[1])
	}
}

func TestSelectRollbackReleaseChoosesPreviousDistinctRelease(t *testing.T) {
	record, err := selectRollbackRelease([]ReleaseRecord{
		{ID: "r1"},
		{ID: "r2"},
		{ID: "r2"},
	}, "")
	if err != nil {
		t.Fatalf("selectRollbackRelease: %v", err)
	}
	if record.ID != "r1" {
		t.Fatalf("expected previous distinct release r1, got %q", record.ID)
	}
}

func TestReleaseRecordJSONTagsUseStableNames(t *testing.T) {
	payload, err := json.Marshal(ReleaseRecord{
		ID:          "r1",
		Slot:        "a",
		Port:        3001,
		Image:       "img:r1",
		ArtifactSHA: "abc123",
		ActivatedAt: "2026-03-08T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(payload)
	for _, expected := range []string{
		`"id":"r1"`,
		`"slot":"a"`,
		`"port":3001`,
		`"image":"img:r1"`,
		`"artifactSha":"abc123"`,
		`"activatedAt":"2026-03-08T10:00:00Z"`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("marshal output missing %q in %s", expected, got)
		}
	}
}
