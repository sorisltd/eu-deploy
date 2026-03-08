package main

import "github.com/sorisltd/eu-deploy/internal/deploy"

type deployPhaseDefinition struct {
	ID    string
	Label string
}

type jsonPhaseState struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
}

type jsonReleaseView struct {
	deploy.ReleaseRecord
	Current bool `json:"current"`
}

func dockerDeployPhaseDefinitions() []deployPhaseDefinition {
	return []deployPhaseDefinition{
		{ID: "buildArtifact", Label: "Build artifact"},
		{ID: "buildImage", Label: "Build Docker image"},
		{ID: "startContainer", Label: "Start container"},
	}
}

func remoteDeployPhaseDefinitions() []deployPhaseDefinition {
	return []deployPhaseDefinition{
		{ID: "buildArtifact", Label: "Build artifact"},
		{ID: "uploadRelease", Label: "Upload release"},
		{ID: "activateRelease", Label: "Activate release"},
	}
}

func phaseStates(defs []deployPhaseDefinition, completedCount, failedIndex int) []jsonPhaseState {
	phases := make([]jsonPhaseState, 0, len(defs))
	for i, def := range defs {
		status := "pending"
		if failedIndex >= 0 && i == failedIndex {
			status = "failed"
		} else if i < completedCount {
			status = "completed"
		}
		phases = append(phases, jsonPhaseState{
			ID:     def.ID,
			Label:  def.Label,
			Status: status,
		})
	}
	return phases
}

func completedPhaseData(defs []deployPhaseDefinition) map[string]any {
	return map[string]any{
		"phases": phaseStates(defs, len(defs), -1),
	}
}

func failedPhaseData(defs []deployPhaseDefinition, failedIndex int) map[string]any {
	phases := phaseStates(defs, failedIndex, failedIndex)
	data := map[string]any{
		"phases": phases,
	}
	if failedIndex >= 0 && failedIndex < len(phases) {
		data["phase"] = phases[failedIndex]
	}
	return data
}

func mergeJSONData(parts ...map[string]any) map[string]any {
	merged := map[string]any{}
	for _, part := range parts {
		for key, value := range part {
			merged[key] = value
		}
	}
	return merged
}

func releaseViewsByNewest(records []deploy.ReleaseRecord) []jsonReleaseView {
	views := make([]jsonReleaseView, 0, len(records))
	if len(records) == 0 {
		return views
	}

	current := records[len(records)-1]
	sorted := deploy.ReleaseRecordsByNewest(records)
	for _, record := range sorted {
		views = append(views, jsonReleaseView{
			ReleaseRecord: record,
			Current:       record.ID == current.ID && record.ActivatedAt == current.ActivatedAt,
		})
	}
	return views
}
