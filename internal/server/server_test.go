package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
	"github.com/Sanjeever/computer-mcp/internal/i18n"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProtocolAndLanguages(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			catalog, _ := i18n.Load(lang)
			engine := NewEngine(ctx)
			defer engine.Close()
			server := New("test", catalog, engine)
			st, ct := mcp.NewInMemoryTransports()
			ss, err := server.Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			cs, err := client.Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			list, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(list.Tools) != 10 {
				t.Fatalf("tools=%d", len(list.Tools))
			}
			for _, tool := range list.Tools {
				if tool.Description != catalog.Text(tool.Name) {
					t.Fatalf("untranslated %s", tool.Name)
				}
			}
			result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "computer_status", Arguments: map[string]any{"unknown": true}})
			if err == nil && (result == nil || !result.IsError) {
				t.Fatal("unknown input accepted")
			}
			result, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "computer_key", Arguments: map[string]any{"window_id": "fake", "keys": []string{"not-a-key"}}})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatal("invalid key accepted")
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, "code") {
				t.Fatal("missing structured error code")
			}
			for _, args := range []map[string]any{
				{"snapshot_id": "missing", "from": map[string]any{"x": 0, "y": 0}, "to": map[string]any{"x": 10, "y": 10}},
				{"snapshot_id": "missing", "path": []map[string]any{{"x": 0, "y": 0}, {"x": 10, "y": 10}}},
				{"snapshot_id": "missing", "path": []map[string]any{{"x": 0, "y": 0}, {"x": 10, "y": 10}}, "hold_keys": []string{"space", "shift"}, "observe_after": map[string]any{"screenshot": true}},
			} {
				result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "computer_drag", Arguments: args})
				if err != nil || !result.IsError {
					t.Fatalf("drag schema: %v %+v", err, result)
				}
				var got struct{ Code string }
				if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
					t.Fatal(err)
				}
				if got.Code != "stale_snapshot" {
					t.Fatalf("valid drag rejected before snapshot validation: %+v", got)
				}
			}
			for _, name := range []string{"computer_click", "computer_scroll", "computer_move"} {
				args := map[string]any{"snapshot_id": "missing", "point": map[string]any{"x": 10, "y": 10}}
				if name == "computer_move" {
					args["hover_ms"] = 600
				} else {
					args["hold_keys"] = []string{"ctrl"}
					if name == "computer_scroll" {
						args["delta_y"] = 1
					} else {
						args["observe_after"] = map[string]any{"screenshot": true}
					}
				}
				result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
				if err != nil || result == nil || !result.IsError {
					t.Fatalf("%s schema: %v %+v", name, err, result)
				}
				var got struct{ Code string }
				if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil || got.Code != "stale_snapshot" {
					t.Fatalf("%s rejected before snapshot validation: %+v %v", name, got, err)
				}
			}
			for _, readOnly := range []bool{true, false} {
				for _, failure := range []error{context.DeadlineExceeded, context.Canceled} {
					register(server, engine, catalog, "probe", readOnly, func(context.Context, *state, StatusArgs) response { return response{err: failure} })
					result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "probe", Arguments: map[string]any{}})
					if err != nil || !result.IsError {
						t.Fatalf("error response: %v %+v", err, result)
					}
					var got struct{ Code, Message string }
					if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
						t.Fatal(err)
					}
					code, _ := errorCode(failure)
					key := code
					if readOnly {
						key += "_read_only"
					}
					if got.Code != code || got.Message != catalog.Text(key) {
						t.Fatalf("incorrect delivery semantics: %+v", got)
					}
				}
			}
		})
	}
}

func TestClickObservationProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b := setupClickDesktop()
	e := newEngine(ctx, func() (desktop.Backend, error) { return b, nil })
	defer e.Close()
	catalog, _ := i18n.Load("zh-CN")
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New("test", catalog, e).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || r.IsError {
			t.Fatalf("%s: %+v, %v", name, r, err)
		}
		return r
	}
	var snap Snapshot
	r := call("computer_observe", nil)
	if err := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &snap); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"snapshot_id": snap.ID, "element_id": "0", "observe_after": map[string]any{"screenshot": true}}
	r = call("computer_click", args)
	var clicked mouseObservationResult
	if err := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &clicked); err != nil {
		t.Fatal(err)
	}
	if !clicked.InputDelivered || clicked.Observation == nil || clicked.Observation.ID == snap.ID || len(r.Content) != 2 {
		t.Fatalf("click result: %+v, content=%+v", clicked, r.Content)
	}
	picture, ok := r.Content[1].(*mcp.ImageContent)
	if !ok || picture.MIMEType != "image/jpeg" || len(picture.Data) == 0 {
		t.Fatalf("missing JPEG content: %+v", r.Content[1])
	}
	args["snapshot_id"] = clicked.Observation.ID
	b.captureErr = desktop.Fail("native_error", "capture failed")
	r = call("computer_click", args)
	clicked = mouseObservationResult{}
	if err := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &clicked); err != nil {
		t.Fatal(err)
	}
	if !clicked.InputDelivered || clicked.Observation != nil || len(r.Content) != 1 || clicked.ObservationError == nil ||
		clicked.ObservationError.Code != "native_error" || clicked.ObservationError.Message != catalog.Text("input_observation_failed") {
		t.Fatalf("click result after capture failure: %+v", clicked)
	}
}
