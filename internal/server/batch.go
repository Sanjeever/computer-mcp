package server

import (
	"context"
	"fmt"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

const (
	maxBatchSteps      = 8
	maxBatchPoints     = 256
	maxBatchDurationMS = 6000
)

type BatchArgs struct {
	SnapshotID string          `json:"snapshot_id"`
	Steps      []batchStepArgs `json:"steps"`
}

type batchStepArgs struct {
	Op       string          `json:"op"`
	Path     []desktop.Point `json:"path"`
	Duration int             `json:"duration_ms,omitempty"`
}

type batchStepResult struct {
	Status        string `json:"status"`
	InputDelivery string `json:"input_delivery"`
}

type batchResult struct {
	Steps     []batchStepResult `json:"steps"`
	StoppedAt *int              `json:"stopped_at,omitempty"`
	Failure   *toolFailure      `json:"error,omitempty"`
}

func batchResults(total, completed int) batchResult {
	r := batchResult{Steps: make([]batchStepResult, total)}
	for i := range r.Steps {
		r.Steps[i] = batchStepResult{Status: "not_run", InputDelivery: "not_sent"}
	}
	for i := range completed {
		r.Steps[i] = batchStepResult{Status: "completed", InputDelivery: "sent"}
	}
	return r
}

func batchStopped(total, index int, delivery string, err error) response {
	r := batchResults(total, index)
	r.StoppedAt = &index
	r.Steps[index] = batchStepResult{Status: "failed", InputDelivery: delivery}
	return response{value: r, err: err}
}

func batch(ctx context.Context, s *state, a BatchArgs) response {
	if len(a.Steps) == 0 || len(a.Steps) > maxBatchSteps {
		return invalid(fmt.Sprintf("steps: 1..%d", maxBatchSteps))
	}
	snap, err := s.latestSnapshot(a.SnapshotID)
	if err != nil {
		return response{err: err}
	}
	if snap.Window == nil || snap.Window.Menu || snap.ImageWidth == 0 || snap.ImageHeight == 0 {
		return invalid("computer_batch requires a window screenshot; observe the drawing window with screenshot=true")
	}
	inputs, err := prepareBatch(ctx, snap, a.Steps)
	if err != nil {
		return response{err: err}
	}
	for i, input := range inputs {
		if err := checkBatchStroke(ctx, s, snap, input.Path); err != nil {
			return batchStopped(len(inputs), i, "not_sent", err)
		}
		s.snapshot = nil
		desktop.ReportStage(ctx, "input")
		if err := s.backend.Input(ctx, "drag", input); err != nil {
			return batchStopped(len(inputs), i, "unknown", err)
		}
	}
	return response{value: batchResults(len(inputs), len(inputs))}
}

func checkBatchStroke(ctx context.Context, s *state, snap *Snapshot, path []desktop.Point) error {
	if err := snap.checkExpiry(); err != nil {
		return err
	}
	desktop.ReportStage(ctx, "hit_test")
	if err := checkDragPath(ctx, s.backend, snap.Window, path); err != nil {
		return err
	}
	// Hit tests can block in a native provider. Recheck the window before dispatch.
	desktop.ReportStage(ctx, "window_list")
	if err := s.validateSnapshotTarget(snap); err != nil {
		return err
	}
	if err := snap.checkExpiry(); err != nil {
		return err
	}
	return ctx.Err()
}

func prepareBatch(ctx context.Context, snap *Snapshot, steps []batchStepArgs) ([]desktop.Input, error) {
	inputs := make([]desktop.Input, len(steps))
	var totalPoints, totalDuration int
	for i, step := range steps {
		if step.Op != "drag" {
			return nil, desktop.Fail("invalid_argument", fmt.Sprintf("steps[%d].op must be drag", i))
		}
		duration, err := dragDuration(step.Path, step.Duration)
		if err != nil {
			return nil, batchArgumentError(i, err)
		}
		totalPoints += len(step.Path)
		totalDuration += duration
		if totalPoints > maxBatchPoints || totalDuration > maxBatchDurationMS {
			return nil, desktop.Fail("invalid_argument", fmt.Sprintf("batch permits at most %d path points and %d ms total duration", maxBatchPoints, maxBatchDurationMS))
		}
		points, err := mapDragPath(ctx, snap, step.Path)
		if err != nil {
			return nil, batchArgumentError(i, err)
		}
		inputs[i] = desktop.Input{Path: points, Duration: duration}
	}
	return inputs, nil
}

func batchArgumentError(index int, err error) error {
	code, detail := errorCode(err)
	if code == "invalid_argument" {
		return desktop.Fail(code, fmt.Sprintf("steps[%d]: %s", index, detail))
	}
	return err
}
