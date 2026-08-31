package generation

import "fmt"

type Status string

type Stage string

const (
	StatusQueued    Status = "QUEUED"
	StatusRunning   Status = "RUNNING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
	StatusCanceled  Status = "CANCELED"
)

const (
	StageQueued             Stage = "QUEUED"
	StageOptimizingPrompt   Stage = "OPTIMIZING_PROMPT"
	StageSubmittingProvider Stage = "SUBMITTING_PROVIDER"
	StageGenerating         Stage = "GENERATING"
	StageFetchingOutput     Stage = "FETCHING_OUTPUT"
	StageStoringOutput      Stage = "STORING_OUTPUT"
	StageCompleted          Stage = "COMPLETED"
)

var transitions = map[Status]map[Status]bool{
	StatusQueued: {
		StatusRunning:  true,
		StatusFailed:   true,
		StatusCanceled: true,
	},
	StatusRunning: {
		StatusSucceeded: true,
		StatusFailed:    true,
		StatusCanceled:  true,
	},
	StatusFailed: {
		StatusQueued: true,
	},
}

func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusCanceled
}

func (s Stage) Valid() bool {
	switch s {
	case StageQueued, StageOptimizingPrompt, StageSubmittingProvider, StageGenerating, StageFetchingOutput, StageStoringOutput, StageCompleted:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Status) bool {
	return transitions[from][to]
}

func ValidateTransition(from, to Status, retry bool) error {
	if !from.Valid() || !to.Valid() {
		return fmt.Errorf("unknown generation job status transition %q -> %q", from, to)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid generation job status transition %q -> %q", from, to)
	}
	if from == StatusFailed && to == StatusQueued && !retry {
		return fmt.Errorf("transition %q -> %q requires an explicit retry", from, to)
	}
	return nil
}

func ValidateProgress(status Status, progress int) error {
	if progress < 0 || progress > 100 {
		return fmt.Errorf("progress must be between 0 and 100")
	}
	if status == StatusSucceeded && progress != 100 {
		return fmt.Errorf("succeeded job progress must be 100")
	}
	return nil
}
