package generation

import "testing"

func TestGenerationJobTransitions(t *testing.T) {
	tests := []struct {
		name     string
		from, to Status
		retry    bool
		wantErr  bool
	}{
		{"queued starts", StatusQueued, StatusRunning, false, false},
		{"queued fails", StatusQueued, StatusFailed, false, false},
		{"queued cancels", StatusQueued, StatusCanceled, false, false},
		{"running succeeds", StatusRunning, StatusSucceeded, false, false},
		{"running fails", StatusRunning, StatusFailed, false, false},
		{"running cancels", StatusRunning, StatusCanceled, false, false},
		{"explicit retry", StatusFailed, StatusQueued, true, false},
		{"implicit retry rejected", StatusFailed, StatusQueued, false, true},
		{"success is terminal", StatusSucceeded, StatusRunning, false, true},
		{"canceled is terminal", StatusCanceled, StatusQueued, true, true},
		{"unknown rejected", Status("UNKNOWN"), StatusQueued, false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTransition(test.from, test.to, test.retry)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateTransition(%q, %q, %v) error = %v, wantErr = %v", test.from, test.to, test.retry, err, test.wantErr)
			}
		})
	}
}

func TestGenerationJobProgress(t *testing.T) {
	tests := []struct {
		status   Status
		progress int
		valid    bool
	}{
		{StatusQueued, 0, true},
		{StatusRunning, 50, true},
		{StatusSucceeded, 100, true},
		{StatusSucceeded, 99, false},
		{StatusRunning, -1, false},
		{StatusRunning, 101, false},
	}
	for _, test := range tests {
		if err := ValidateProgress(test.status, test.progress); (err == nil) != test.valid {
			t.Errorf("ValidateProgress(%q, %d) error = %v, want valid = %v", test.status, test.progress, err, test.valid)
		}
	}
}

func TestAllStagesAreValid(t *testing.T) {
	stages := []Stage{StageQueued, StageOptimizingPrompt, StageSubmittingProvider, StageGenerating, StageFetchingOutput, StageStoringOutput, StageCompleted}
	for _, stage := range stages {
		if !stage.Valid() {
			t.Errorf("stage %q must be valid", stage)
		}
	}
	if Stage("UNKNOWN").Valid() {
		t.Fatal("unknown stage must be invalid")
	}
}
