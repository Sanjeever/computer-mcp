package desktop

import "context"

type progressKey struct{}

// WithStageReporter lets callers diagnose deadlines while a native provider
// still occupies the worker.
func WithStageReporter(ctx context.Context, report func(string)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}

func ReportStage(ctx context.Context, stage string) {
	if report, ok := ctx.Value(progressKey{}).(func(string)); ok {
		report(stage)
	}
}
