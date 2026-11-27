package server

import (
	"context"
	"encoding/json"
	"runtime"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
	"github.com/Sanjeever/computer-mcp/internal/i18n"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolFailure struct {
	Code            string        `json:"code"`
	Message         string        `json:"message"`
	Detail          string        `json:"detail,omitempty"`
	Timing          *stageTiming  `json:"timing,omitempty"`
	SuggestedRegion *desktop.Rect `json:"suggested_region,omitempty"`
}

func New(version string, c i18n.Catalog, e *Engine) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "computer-mcp", Version: version}, &mcp.ServerOptions{Instructions: c.Text("instructions")})
	register(s, e, c, "computer_status", true, status)
	register(s, e, c, "computer_window", false, window)
	register(s, e, c, "computer_observe", true, observe)
	register(s, e, c, "computer_click", false, click)
	register(s, e, c, "computer_move", false, move)
	register(s, e, c, "computer_type", false, typeText)
	register(s, e, c, "computer_key", false, key)
	register(s, e, c, "computer_scroll", false, scroll)
	register(s, e, c, "computer_drag", false, drag)
	register(s, e, c, "computer_batch", false, batch)
	register(s, e, c, "computer_wait", true, wait)
	return s
}
func register[A any](s *mcp.Server, e *Engine, c i18n.Catalog, name string, readOnly bool, f func(context.Context, *state, A) response) {
	schema, err := jsonschema.For[A](nil)
	if err != nil {
		panic(err)
	}
	schema.AdditionalProperties = &jsonschema.Schema{Not: &jsonschema.Schema{}}
	for k, p := range schema.Properties {
		if d, ok := c[k]; ok {
			p.Description = d
		}
	}
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: c.Text(name), InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly}}, func(ctx context.Context, _ *mcp.CallToolRequest, a A) (*mcp.CallToolResult, any, error) {
		r := e.call(ctx, name, func(ctx context.Context, s *state) response { return f(ctx, s, a) })
		batch, isBatch := r.value.(batchResult)
		if r.err != nil {
			code, detail := errorCode(r.err)
			message := c.Text(code)
			if readOnly && (code == "timeout" || code == "canceled") {
				message = c.Text(code + "_read_only")
			}
			if runtime.GOOS == "windows" && code == "target_obscured" {
				message = c.Text("target_obscured_windows")
			}
			v := toolFailure{Code: code, Message: message, Detail: detail, Timing: r.timing}
			if !isBatch {
				data, _ := json.Marshal(v)
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
			}
			batch.Failure = &v
		}
		var transitionImage []byte
		if v, ok := r.value.(mouseObservationResult); ok {
			if v.err != nil {
				v.ObservationError = observationFailure(c, v.err, r.timing, v.suggestedRegion)
			}
			if v.transitionErr != nil {
				code, detail := errorCode(v.transitionErr)
				v.TransitionError = &toolFailure{Code: code, Message: c.Text("input_transition_failed"), Detail: detail, Timing: r.timing}
			}
			transitionImage = v.transitionImage
			r.value = v
		}
		if isBatch {
			if batch.observationErr != nil {
				batch.ObservationError = observationFailure(c, batch.observationErr, r.timing, batch.suggestedRegion)
			}
			r.value = batch
		}
		data, err := json.Marshal(r.value)
		if err != nil {
			return nil, nil, err
		}
		result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
		if len(r.image) > 0 {
			result.Content = append(result.Content, &mcp.ImageContent{Data: r.image, MIMEType: "image/jpeg"})
		}
		if len(transitionImage) > 0 {
			result.Content = append(result.Content, &mcp.ImageContent{Data: transitionImage, MIMEType: "image/jpeg"})
		}
		if r.err != nil {
			result.IsError = true
			result.StructuredContent = r.value
			return result, nil, nil
		}
		return result, r.value, nil
	})
}

func observationFailure(c i18n.Catalog, err error, timing *stageTiming, region *desktop.Rect) *toolFailure {
	code, detail := errorCode(err)
	return &toolFailure{Code: code, Message: c.Text("input_observation_failed"), Detail: detail, Timing: timing, SuggestedRegion: region}
}
