package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCatalogCoversOfficialOperations(t *testing.T) {
	count := 0
	for endpoint, methods := range catalog().Paths {
		for method := range methods {
			if method == "parameters" {
				continue
			}
			if !contains([]string{"get", "post", "put", "patch", "delete"}, method) {
				t.Fatalf("unsupported catalog method %s", method)
			}
			if _, err := operation(endpoint, method); err != nil {
				t.Fatal(err)
			}
			count++
		}
	}
	if count < 261 {
		t.Fatalf("catalog incomplete: %d", count)
	}
	for _, path := range []string{"/boards/", "/cards", "/members/{id}", "/organizations/{id}", "/webhooks/{id}", "/enterprises/{id}", "/tokens/{token}", "/search", "/batch"} {
		if len(catalog().Paths[path]) == 0 {
			t.Fatal("missing resource", path)
		}
	}
}

func TestGenericReadScopeAndCredentials(t *testing.T) {
	requests := 0
	c := fakeClient(t, func(r *http.Request) (int, string) {
		requests++
		if r.URL.Host != "api.trello.com" || r.Method != "GET" {
			t.Fatal("wrong host/method")
		}
		if r.URL.Query().Get("key") != "test-key" || r.URL.Query().Get("token") != "test-token" {
			t.Fatal("missing injected auth")
		}
		return 200, `{"id":"allowed","name":"Example"}`
	})
	in := apiWriteInput{Endpoint: "/boards/{id}", Method: "GET", PathParams: map[string]string{"id": "allowed"}, Query: map[string]any{"fields": "name"}}
	if _, err := c.callAPI(in); err != nil {
		t.Fatal(err)
	}
	in.PathParams["id"] = "foreign"
	if _, err := c.callAPI(in); err == nil {
		t.Fatal("foreign board accepted")
	}
	in.PathParams["id"] = "allowed"
	in.Query["token"] = "override"
	if _, err := c.callAPI(in); err == nil {
		t.Fatal("credential override accepted")
	}
	in.Query = nil
	in.PathParams["id"] = "../members/me"
	if _, err := c.callAPI(in); err == nil {
		t.Fatal("path escape accepted")
	}
	in.Endpoint = "/members/{id}"
	in.PathParams["id"] = "me"
	if _, err := c.callAPI(in); err == nil {
		t.Fatal("account read accepted in board mode")
	}
	if requests != 1 {
		t.Fatal("rejected requests reached network", requests)
	}
	c.cfg.APIScope = "account"
	if _, err := c.callAPI(in); err != nil {
		t.Fatal("account mode rejected catalog operation", err)
	}
}

func TestGenericWritesJSONFormMultipartAndNoContent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, encoding := range []string{"json", "form", "multipart"} {
		t.Run(encoding, func(t *testing.T) {
			c := fakeClient(t, func(r *http.Request) (int, string) {
				if r.Method != "POST" {
					t.Fatal("wrong method")
				}
				switch encoding {
				case "json":
					var b map[string]any
					if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b["name"] != "Example" {
						t.Fatal("bad JSON body", err)
					}
				case "form":
					if err := r.ParseForm(); err != nil || r.PostForm.Get("name") != "Example" {
						t.Fatal("bad form")
					}
				case "multipart":
					if err := r.ParseMultipartForm(1024); err != nil {
						t.Fatal(err)
					}
					f, _, err := r.FormFile("file")
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					b, _ := io.ReadAll(f)
					if string(b) != "fixture" || r.FormValue("name") != "Example" {
						t.Fatal("bad upload")
					}
				}
				return 204, ""
			})
			c.cfg.APIScope = "account"
			in := apiWriteInput{Endpoint: "/cards", Method: "POST", Body: map[string]any{"name": "Example"}, BodyEncoding: encoding}
			if encoding == "multipart" {
				in.Endpoint = "/cards/{id}/attachments"
				in.PathParams = map[string]string{"id": "card1"}
				in.Files = map[string]string{"file": file}
			}
			out, err := c.callAPI(in)
			if err != nil || out.Status != 204 || out.Data != nil {
				t.Fatal("empty success mishandled", err)
			}
		})
	}
}

func TestGenericRejectsForeignDestination(t *testing.T) {
	c := fakeClient(t, func(r *http.Request) (int, string) {
		if r.Method != "GET" {
			t.Fatal("scope bypass wrote data")
		}
		if r.URL.Path == "/1/cards/card1" {
			return 200, `{"idBoard":"allowed"}`
		}
		return 200, `{"idBoard":"foreign"}`
	})
	in := apiWriteInput{Endpoint: "/cards/{id}", Method: "PUT", PathParams: map[string]string{"id": "card1"}, Body: map[string]any{"idList": "foreignList"}}
	if _, err := c.callAPI(in); err == nil {
		t.Fatal("foreign destination accepted")
	}
}

func TestMCPToolsAndLockedBoardSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := fakeClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/1/boards/allowed" {
			return 200, `{"id":"allowed","name":"Example"}`
		}
		if r.URL.Path == "/1/cards/card1" {
			return 200, `{"idBoard":"allowed"}`
		}
		if r.URL.Path == "/1/cards/card1/actions" {
			return 200, `[]`
		}
		t.Fatal("unexpected endpoint", r.URL.Path)
		return 500, ""
	})
	c.cfg.BoardIDsLocked = true
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerTools(server, c)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cc := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := cc.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, name := range []string{"list_card_comments", "list_card_history", "list_card_review_history"} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{"card_id": "card1"}})
		if err != nil || r.IsError {
			t.Fatal("read tool failed", name, err)
		}
	}
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "select_allowed_boards", Arguments: map[string]any{"board_ids": []string{"foreign"}}})
	if err != nil || !r.IsError {
		t.Fatal("locked scope was not enforced", err)
	}
	r, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_available_boards", Arguments: map[string]any{}})
	if err != nil || r.IsError {
		t.Fatal("allowed board discovery failed", err)
	}
	r, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "search_api_endpoints", Arguments: map[string]any{"keyword": "", "limit": 100}})
	if err != nil || r.IsError {
		t.Fatal("catalog unavailable", err)
	}
	r, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_api_endpoint", Arguments: map[string]any{"endpoint": "/cards/{id}", "method": "GET"}})
	if err != nil || r.IsError {
		t.Fatal("schema unavailable", err)
	}
	if !strings.Contains(r.Content[0].(*mcp.TextContent).Text, "definitions") {
		t.Fatal("missing definitions")
	}
}
