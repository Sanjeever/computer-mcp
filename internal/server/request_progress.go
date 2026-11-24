package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

type stageTiming struct {
	Stage     string `json:"stage"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

type requestStage struct {
	name  string
	start time.Time
}

type requestProgress struct {
	current atomic.Pointer[requestStage]
	tool    string
	log     bool
}

func (p *requestProgress) begin(name string) {
	previous := p.current.Load()
	if previous != nil && previous.name == name {
		return
	}
	now := time.Now()
	p.current.Store(&requestStage{name: name, start: now})
	if p.log && previous != nil {
		p.write(previous.timing(now))
	}
}

func (p *requestProgress) timing() stageTiming {
	return p.current.Load().timing(time.Now())
}

func (s *requestStage) timing(now time.Time) stageTiming {
	return stageTiming{Stage: s.name, ElapsedMS: now.Sub(s.start).Milliseconds()}
}

func (p *requestProgress) write(timing stageTiming) {
	fmt.Fprintf(os.Stderr, "computer-mcp timing tool=%s stage=%s elapsed_ms=%d\n", p.tool, timing.Stage, timing.ElapsedMS)
}

func (j *job) finish(r response) {
	if j.progress.log {
		j.progress.write(j.progress.timing())
	}
	j.result <- r
}

func (j *job) withTiming(r response) response {
	err := r.err
	if result, ok := r.value.(mouseObservationResult); ok {
		err = result.err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		timing := j.progress.timing()
		r.timing = &timing
	}
	return r
}
