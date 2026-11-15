package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
			if len(list.Tools) != 9 {
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
