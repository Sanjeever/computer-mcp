package desktop

import (
	"context"
	"time"
)

func moveDrag(ctx context.Context, in Input, move func(Point) error) (Point, error) {
	p := in.Path[0]
	segments := len(in.Path) - 1
	steps := max(1, in.Duration/(16*segments))
	total := steps * segments
	duration := time.Duration(in.Duration) * time.Millisecond
	start := time.Now()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for segment := range segments {
		from, to := in.Path[segment], in.Path[segment+1]
		for step := 1; step <= steps; step++ {
			if err := ctx.Err(); err != nil {
				return p, err
			}
			deadline := start.Add(duration * time.Duration(segment*steps+step) / time.Duration(total))
			timer.Reset(time.Until(deadline))
			select {
			case <-ctx.Done():
				return p, ctx.Err()
			case <-timer.C:
			}
			if err := ctx.Err(); err != nil {
				return p, err
			}
			t := float64(step) / float64(steps)
			p = Point{from.X + (to.X-from.X)*t, from.Y + (to.Y-from.Y)*t}
			if step == steps {
				p = to
			}
			if err := move(p); err != nil {
				return p, err
			}
		}
	}
	return p, nil
}
