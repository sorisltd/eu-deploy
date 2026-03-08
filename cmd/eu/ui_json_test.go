package main

import (
	"testing"

	"github.com/sorisltd/eu-deploy/internal/deploy"
)

func TestPhaseStatesMarksCompletedAndFailedPhases(t *testing.T) {
	phases := phaseStates(remoteDeployPhaseDefinitions(), 2, 2)
	if len(phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(phases))
	}
	if phases[0].ID != "buildArtifact" || phases[0].Status != "completed" {
		t.Fatalf("unexpected first phase: %#v", phases[0])
	}
	if phases[1].ID != "uploadRelease" || phases[1].Status != "completed" {
		t.Fatalf("unexpected second phase: %#v", phases[1])
	}
	if phases[2].ID != "activateRelease" || phases[2].Status != "failed" {
		t.Fatalf("unexpected third phase: %#v", phases[2])
	}
}

func TestReleaseViewsByNewestMarksCurrentRecord(t *testing.T) {
	views := releaseViewsByNewest([]deploy.ReleaseRecord{
		{ID: "r1", ActivatedAt: "2026-03-08T10:00:00Z"},
		{ID: "r2", ActivatedAt: "2026-03-08T10:05:00Z"},
		{ID: "r3", ActivatedAt: "2026-03-08T10:10:00Z"},
	})
	if len(views) != 3 {
		t.Fatalf("expected 3 views, got %d", len(views))
	}
	if views[0].ID != "r3" || !views[0].Current {
		t.Fatalf("expected newest release to be current, got %#v", views[0])
	}
	if views[1].Current || views[2].Current {
		t.Fatalf("expected only one current release, got %#v", views)
	}
}
