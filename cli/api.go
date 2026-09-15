package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed trello-openapi.json
var apiSchema []byte

type apiCatalog struct {
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}
type apiEndpointInput struct {
	Endpoint string `json:"endpoint" jsonschema:"Exact path template from search_api_endpoints, e.g. /cards/{id}"`
	Method   string `json:"method" jsonschema:"HTTP method from the endpoint catalog"`
}
type apiSearchInput struct {
	Keyword string `json:"keyword,omitempty" jsonschema:"Words to match path or operation summary; empty lists all"`
	Method  string `json:"method,omitempty" jsonschema:"Optional HTTP method filter"`
	Offset  int    `json:"offset,omitempty" jsonschema:"Result offset, default zero"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Result count 1-100, default 20"`
}
type apiReadInput struct {
	Endpoint   string            `json:"endpoint" jsonschema:"Exact GET path template returned by search_api_endpoints"`
	PathParams map[string]string `json:"path_params,omitempty" jsonschema:"Values for path placeholders, without URLs or slash characters"`
	Query      map[string]any    `json:"query,omitempty" jsonschema:"Query parameters from get_api_endpoint; credentials are injected automatically"`
}
type apiWriteInput struct {
	Endpoint     string            `json:"endpoint" jsonschema:"Exact path template returned by search_api_endpoints"`
	Method       string            `json:"method" jsonschema:"POST, PUT, PATCH or DELETE; never use for a planning run"`
	PathParams   map[string]string `json:"path_params,omitempty" jsonschema:"Values for path placeholders"`
	Query        map[string]any    `json:"query,omitempty" jsonschema:"Query parameters, excluding credentials"`
	Body         map[string]any    `json:"body,omitempty" jsonschema:"Request body fields from the endpoint schema"`
	BodyEncoding string            `json:"body_encoding,omitempty" jsonschema:"json (default) or form; files automatically use multipart"`
	Files        map[string]string `json:"files,omitempty" jsonschema:"Multipart field name to absolute local file path, only for documented upload endpoints"`
}
type apiResponse struct {
	BatchComplete      *bool    `json:"batch_complete,omitempty"`
	BatchRoutes        []string `json:"batch_routes,omitempty"`
	BatchFailedIndexes []int    `json:"batch_failed_indexes,omitempty"`
	Status             int      `json:"status"`
	Data               any      `json:"data,omitempty"`
	Encoding           string   `json:"encoding,omitempty"`
	ContentType        string   `json:"content_type,omitempty"`
}

var catalogOnce sync.Once
var cachedCatalog apiCatalog

func catalog() apiCatalog {
	catalogOnce.Do(func() {
		if err := json.Unmarshal(apiSchema, &cachedCatalog); err != nil {
			panic("invalid bundled Trello API schema")
		}
	})
	return cachedCatalog
}
func operation(endpoint, method string) (map[string]any, error) {
	raw := catalog().Paths[endpoint][strings.ToLower(method)]
	if len(raw) == 0 {
		return nil, fmt.Errorf("Endpoint/method not in bundled Trello REST catalog; use search_api_endpoints")
	}
	var op map[string]any
	if err := json.Unmarshal(raw, &op); err != nil {
		return nil, err
	}
	return op, nil
}
func resolveAPIPath(endpoint string, params map[string]string) (string, error) {
	path := endpoint
	for _, part := range strings.Split(endpoint, "/") {
		if !strings.HasPrefix(part, "{") {
			continue
		}
		key := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
		v := params[key]
		if v == "" || strings.ContainsAny(v, "/\\?#%") || v == "." || v == ".." {
			return "", fmt.Errorf("Invalid or missing path parameter %s", key)
		}
		path = strings.ReplaceAll(path, part, url.PathEscape(v))
	}
	return path, nil
}

// Board mode fails closed for account-wide resources. Batch validates each child route.
// Full API access requires an explicit local TRELLO_API_SCOPE=account setting.
func (c *TrelloClient) resourceBoard(kind, id string) (string, error) {
	key := kind + "/" + id
	if board, ok := c.scopeCache[key]; ok {
		return board, nil
	}
	board, err := c.resolveResourceBoard(kind, id)
	if err == nil && c.scopeCache != nil {
		c.scopeCache[key] = board
	}
	return board, err
}
func (c *TrelloClient) resolveResourceBoard(kind, id string) (string, error) {
	if !cardReference.MatchString(id) {
		return "", fmt.Errorf("Resource reference must be an id or shortlink")
	}
	if kind == "boards" {
		return id, nil
	}
	if kind == "checklists" {
		var v struct {
			IDCard string `json:"idCard"`
		}
		err := c.request(http.MethodGet, "/checklists/"+id, map[string]string{"fields": "idCard", "checkItems": "none"}, &v)
		if err != nil {
			return "", err
		}
		return c.resourceBoard("cards", v.IDCard)
	}
	if kind == "actions" && c.scopeCache != nil {
		var board struct {
			ID string `json:"id"`
		}
		if err := c.request(http.MethodGet, "/actions/"+id+"/board", map[string]string{"fields": "id"}, &board); err != nil {
			return "", err
		}
		if board.ID == "" {
			return "", fmt.Errorf("Cannot establish board scope for this action")
		}
		return board.ID, nil
	}
	var v struct {
		IDBoard   string `json:"idBoard"`
		IDModel   string `json:"idModel"`
		ModelType string `json:"modelType"`
		Data      struct {
			Board struct {
				ID string `json:"id"`
			} `json:"board"`
		} `json:"data"`
	}
	q := map[string]string{"fields": "idBoard"}
	if kind == "customFields" || kind == "actions" {
		q = nil
	}
	if err := c.request(http.MethodGet, "/"+kind+"/"+id, q, &v); err != nil {
		return "", err
	}
	if kind == "actions" {
		v.IDBoard = v.Data.Board.ID
	}
	if kind == "customFields" && v.ModelType == "board" {
		v.IDBoard = v.IDModel
	}
	if v.IDBoard == "" {
		return "", fmt.Errorf("Cannot establish board scope for this resource")
	}
	return v.IDBoard, nil
}
func (c *TrelloClient) checkResource(kind, id string) error {
	board, err := c.resourceBoard(kind, id)
	if err != nil {
		return err
	}
	return ensureBoardAccess(c.cfg, board)
}
func (c *TrelloClient) checkReferences(values map[string]any) error {
	for key, value := range values {
		lower := strings.ToLower(key)
		if lower == "idorganization" || lower == "identerprise" || lower == "urlsource" {
			return fmt.Errorf("This parameter requires account API scope")
		}
		kind := ""
		for prefix, resource := range map[string]string{"idboard": "boards", "idlist": "lists", "idcard": "cards", "idchecklist": "checklists", "idlabel": "labels", "idcustomfield": "customFields"} {
			if strings.HasPrefix(lower, prefix) {
				kind = resource
				break
			}
		}
		if kind != "" {
			var ids []string
			switch v := value.(type) {
			case string:
				ids = strings.Split(v, ",")
			case []any:
				for _, id := range v {
					s, ok := id.(string)
					if !ok {
						return fmt.Errorf("Resource IDs must be strings")
					}
					ids = append(ids, s)
				}
			default:
				return fmt.Errorf("Resource IDs must be strings")
			}
			for _, id := range ids {
				if err := c.checkResource(kind, id); err != nil {
					return err
				}
			}
		}
		switch v := value.(type) {
		case map[string]any:
			if err := c.checkReferences(v); err != nil {
				return err
			}
		case []any:
			for _, item := range v {
				if obj, ok := item.(map[string]any); ok {
					if err := c.checkReferences(obj); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
func (c *TrelloClient) checkAPIScope(path string, in apiWriteInput) error {
	if c.cfg.APIScope == "account" {
		return nil
	}
	if in.Method != "GET" && (strings.Contains(in.Endpoint, "{field}") || strings.HasSuffix(in.Endpoint, "/idBoard")) {
		return fmt.Errorf("Generic field/board-transfer writes require account API scope; use a dedicated scoped tool")
	}
	if err := ensureOnboardingComplete(c.cfg); err != nil {
		return err
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return fmt.Errorf("Collection-level operations require account API scope; use a dedicated board-scoped tool when available")
	}
	switch parts[0] {
	case "boards", "cards", "lists", "checklists", "labels", "customFields", "actions":
	default:
		return fmt.Errorf("This endpoint requires TRELLO_API_SCOPE=account; current scope is boards")
	}
	if parts[0] == "boards" && len(parts) > 2 && (parts[2] == "organization" || parts[2] == "memberships") {
		return fmt.Errorf("Workspace/membership expansion requires account API scope")
	}
	if err := c.checkResource(parts[0], parts[1]); err != nil {
		return err
	}
	params := map[string]any{}
	for k, v := range in.PathParams {
		if k != "id" {
			params[k] = v
		}
	}
	for _, values := range []map[string]any{params, in.Query, in.Body} {
		if err := c.checkReferences(values); err != nil {
			return err
		}
	}
	return nil
}
func apiValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := []string{}
		for _, item := range x {
			parts = append(parts, apiValue(item))
		}
		return strings.Join(parts, ",")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
func rejectCredentialParams(values map[string]any) error {
	for key := range values {
		switch strings.ToLower(key) {
		case "key", "token", "authorization", "oauth_token", "oauth_consumer_key":
			return fmt.Errorf("Credentials must come from local configuration, not tool arguments")
		}
	}
	return nil
}
func (c *TrelloClient) callAPI(in apiWriteInput) (*apiResponse, error) {
	method := strings.ToUpper(in.Method)
	if _, err := operation(in.Endpoint, method); err != nil {
		return nil, err
	}
	path, err := resolveAPIPath(in.Endpoint, in.PathParams)
	if err != nil {
		return nil, err
	}
	if err := rejectCredentialParams(in.Query); err != nil {
		return nil, err
	}
	if err := rejectCredentialParams(in.Body); err != nil {
		return nil, err
	}
	var batchRoutes []string
	if path == "/batch" {
		batchRoutes, err = c.prepareBatch(in)
		if err != nil {
			return nil, err
		}
		in.Query = map[string]any{"urls": strings.Join(batchRoutes, ",")}
	} else if err := c.checkAPIScope(path, in); err != nil {
		return nil, err
	}
	u, _ := url.Parse(trelloAPIBase + path)
	q := u.Query()
	q.Set("key", c.cfg.APIKey)
	q.Set("token", c.cfg.Token)
	for k, v := range in.Query {
		q.Set(k, apiValue(v))
	}
	u.RawQuery = q.Encode()
	var body io.Reader
	contentType := ""
	if len(in.Files) > 0 {
		buffer := &bytes.Buffer{}
		writer := multipart.NewWriter(buffer)
		for k, v := range in.Body {
			if err := writer.WriteField(k, apiValue(v)); err != nil {
				return nil, err
			}
		}
		for field, path := range in.Files {
			if !filepath.IsAbs(path) {
				return nil, fmt.Errorf("Upload paths must be absolute")
			}
			file, err := os.Open(path)
			if err != nil {
				return nil, fmt.Errorf("Cannot open upload file")
			}
			part, err := writer.CreateFormFile(field, filepath.Base(path))
			if err == nil {
				_, err = io.Copy(part, file)
			}
			file.Close()
			if err != nil {
				return nil, fmt.Errorf("Cannot read upload file")
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		body = buffer
		contentType = writer.FormDataContentType()
	} else if in.Body != nil {
		switch in.BodyEncoding {
		case "", "json":
			b, err := json.Marshal(in.Body)
			if err != nil {
				return nil, err
			}
			body = bytes.NewReader(b)
			contentType = "application/json"
		case "form":
			v := url.Values{}
			for k, x := range in.Body {
				v.Set(k, apiValue(x))
			}
			body = strings.NewReader(v.Encode())
			contentType = "application/x-www-form-urlencoded"
		default:
			return nil, fmt.Errorf("body_encoding must be json or form")
		}
	}
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("Cannot build Trello request")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, safeTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TrelloAPIError{Status: resp.StatusCode, Message: safeErrorMessage(resp)}
	}
	// Fail explicitly rather than returning a silently truncated response.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil {
		return nil, fmt.Errorf("Cannot read Trello response")
	}
	if len(raw) > 8*1024*1024 {
		return nil, fmt.Errorf("Response exceeds 8 MiB; request fewer fields or a smaller page. If this was a write, verify state before retrying")
	}
	out := &apiResponse{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type")}
	if len(raw) > 0 {
		if json.Unmarshal(raw, &out.Data) == nil {
			out.Encoding = "json"
		} else if strings.HasPrefix(out.ContentType, "text/") {
			out.Data = string(raw)
			out.Encoding = "text"
		} else {
			out.Data = base64.StdEncoding.EncodeToString(raw)
			out.Encoding = "base64"
		}
	}
	if batchRoutes != nil {
		summarizeBatch(out, batchRoutes)
	}
	return out, nil
}

func registerAPITools(server *mcp.Server, client *TrelloClient) {
	mcp.AddTool(server, &mcp.Tool{Name: "search_api_endpoints", Description: "Search the complete bundled official Trello REST API catalog (all resource families). Returns path templates and methods; use get_api_endpoint for parameters. Catalog availability does not override configured board scope or Trello permissions.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, _ *mcp.CallToolRequest, in apiSearchInput) (*mcp.CallToolResult, any, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 20
		}
		if limit < 1 || limit > 100 || in.Offset < 0 {
			return nil, nil, fmt.Errorf("Invalid offset or limit")
		}
		rows := []map[string]string{}
		for path, methods := range catalog().Paths {
			for method := range methods {
				if !contains([]string{"get", "post", "put", "patch", "delete", "head", "options"}, method) {
					continue
				}
				op, _ := operation(path, method)
				summary, _ := op["summary"].(string)
				if in.Method != "" && !strings.EqualFold(in.Method, method) {
					continue
				}
				text := strings.ToLower(path + " " + summary)
				match := true
				for _, word := range strings.Fields(strings.ToLower(in.Keyword)) {
					if !strings.Contains(text, word) {
						match = false
					}
				}
				if match {
					rows = append(rows, map[string]string{"endpoint": path, "method": strings.ToUpper(method), "summary": summary})
				}
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i]["endpoint"]+rows[i]["method"] < rows[j]["endpoint"]+rows[j]["method"]
		})
		total := len(rows)
		start := min(in.Offset, total)
		end := min(start+limit, total)
		return jsonResult(map[string]any{"total": total, "endpoints": rows[start:end], "has_more": end < total, "next_offset": end, "configured_scope": client.cfg.APIScope})
	})
	mcp.AddTool(server, &mcp.Tool{Name: "get_api_endpoint", Description: "Get the official parameters, request/response schema and referenced component definitions for an exact endpoint and method", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, _ *mcp.CallToolRequest, in apiEndpointInput) (*mcp.CallToolResult, any, error) {
		op, err := operation(in.Endpoint, in.Method)
		if err != nil {
			return nil, nil, err
		}
		var root map[string]any
		_ = json.Unmarshal(apiSchema, &root)
		var pathParameters any
		if raw := catalog().Paths[in.Endpoint]["parameters"]; len(raw) > 0 {
			_ = json.Unmarshal(raw, &pathParameters)
		}
		refs := map[string]any{}
		var visit func(any)
		visit = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				if ref, ok := x["$ref"].(string); ok {
					if _, seen := refs[ref]; !seen {
						refs[ref] = nil
						var resolved any = root
						for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
							m, ok := resolved.(map[string]any)
							if !ok {
								resolved = nil
								break
							}
							resolved = m[part]
						}
						refs[ref] = resolved
						if resolved != nil {
							visit(resolved)
						}
					}
				}
				for _, item := range x {
					visit(item)
				}
			case []any:
				for _, item := range x {
					visit(item)
				}
			}
		}
		visit(op)
		visit(pathParameters)
		return jsonResult(map[string]any{"endpoint": in.Endpoint, "method": strings.ToUpper(in.Method), "operation": op, "path_parameters": pathParameters, "definitions": refs, "configured_scope": client.cfg.APIScope})
	})
	mcp.AddTool(server, &mcp.Tool{Name: "trello_api_read", Description: "Execute a GET operation from the official Trello REST catalog. Automatically authenticates; respects configured board scope. Supports /batch with 1-10 validated relative GET routes in query.urls; encode commas within query values as %2C. Inspect batch_complete and per-item results; retry only failed reads. Pagination is explicit.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, _ *mcp.CallToolRequest, in apiReadInput) (*mcp.CallToolResult, any, error) {
		out, err := client.callAPI(apiWriteInput{Endpoint: in.Endpoint, Method: "GET", PathParams: in.PathParams, Query: in.Query})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(out)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "trello_api_write", Description: "Execute POST, PUT, PATCH or DELETE from the official Trello REST catalog, including JSON/form bodies and multipart uploads. Can modify or permanently delete data. Requires approval for the specific target and change; never use in Planning. Do not retry uncertain writes without checking persisted state."}, func(_ context.Context, _ *mcp.CallToolRequest, in apiWriteInput) (*mcp.CallToolResult, any, error) {
		if !contains([]string{"POST", "PUT", "PATCH", "DELETE"}, strings.ToUpper(in.Method)) {
			return nil, nil, fmt.Errorf("Use trello_api_read for GET; write methods are POST, PUT, PATCH, DELETE")
		}
		out, err := client.callAPI(in)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(out)
	})
}
