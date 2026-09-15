package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Parse all routes before scope discovery. Never forward opaque user URLs to /batch.
func parseBatch(in apiWriteInput) ([]apiWriteInput, []string, error) {
	if in.Method != "GET" || len(in.PathParams) > 0 || len(in.Body) > 0 || len(in.Files) > 0 || len(in.Query) != 1 {
		return nil, nil, fmt.Errorf("Batch requires GET and only the urls query parameter")
	}
	raw, ok := in.Query["urls"].(string)
	if !ok || raw == "" {
		return nil, nil, fmt.Errorf("Batch urls must be a comma-separated string of 1-10 relative routes")
	}
	routes := strings.Split(raw, ",")
	if len(routes) > 10 {
		return nil, nil, fmt.Errorf("Batch supports at most 10 routes; encode commas inside query values as %%2C")
	}
	inputs := make([]apiWriteInput, 0, len(routes))
	canonical := make([]string, 0, len(routes))
	for i, route := range routes {
		child, normalized, err := parseBatchRoute(route)
		if err != nil {
			return nil, nil, fmt.Errorf("Batch item %d: %w", i+1, err)
		}
		inputs = append(inputs, child)
		canonical = append(canonical, normalized)
	}
	return inputs, canonical, nil
}

func parseBatchRoute(route string) (apiWriteInput, string, error) {
	bad := func() (apiWriteInput, string, error) {
		return apiWriteInput{}, "", fmt.Errorf("Invalid batch route; use a canonical relative catalog GET path")
	}
	if route == "" || strings.TrimSpace(route) != route || strings.ContainsAny(route, "\\\r\n\t#") || !strings.HasPrefix(route, "/") || strings.HasPrefix(route, "//") {
		return bad()
	}
	u, err := url.Parse(route)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.RawPath != "" || strings.ContainsAny(u.Path, "%") || strings.Contains(u.Path, "//") || strings.HasSuffix(u.Path, "/") {
		return bad()
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return bad()
		}
	}
	if u.Path == "/batch" || strings.HasPrefix(u.Path, "/batch/") {
		return bad()
	}
	// Prefer literal routes over {field} fallbacks, independently of map iteration order.
	templates := make([]string, 0, len(catalog().Paths))
	for p := range catalog().Paths {
		templates = append(templates, p)
	}
	sort.Slice(templates, func(i, j int) bool {
		a, b := strings.Count(templates[i], "{"), strings.Count(templates[j], "{")
		if a != b {
			return a < b
		}
		return templates[i] < templates[j]
	})
	var child apiWriteInput
	var op map[string]any
	for _, p := range templates {
		a, b := strings.Split(p, "/"), strings.Split(u.Path, "/")
		if len(a) != len(b) {
			continue
		}
		params := map[string]string{}
		match := true
		for i, v := range a {
			if strings.HasPrefix(v, "{") {
				params[strings.Trim(v, "{}")] = b[i]
			} else if v != b[i] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		candidate, e := operation(p, "GET")
		if e != nil {
			continue
		}
		resolved, e := resolveAPIPath(p, params)
		if e != nil || resolved != u.Path {
			continue
		}
		child = apiWriteInput{Endpoint: p, Method: "GET", PathParams: params, Query: map[string]any{}}
		op = candidate
		break
	}
	if op == nil {
		return bad()
	}
	pathValues := map[string]any{}
	for k, v := range child.PathParams {
		pathValues[k] = v
	}
	if err := rejectCredentialParams(pathValues); err != nil {
		return apiWriteInput{}, "", err
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return bad()
	}
	for k, v := range q {
		if len(v) != 1 {
			return bad()
		}
		child.Query[k] = v[0]
	}
	if err := rejectCredentialParams(child.Query); err != nil {
		return apiWriteInput{}, "", err
	}
	allowed := map[string]map[string]any{}
	add := func(v any) {
		if ps, ok := v.([]any); ok {
			for _, x := range ps {
				p, ok := x.(map[string]any)
				if !ok {
					continue
				}
				if p["in"] == "query" {
					name, _ := p["name"].(string)
					allowed[name] = p
				}
			}
		}
	}
	add(op["parameters"])
	var shared any
	_ = json.Unmarshal(catalog().Paths[child.Endpoint]["parameters"], &shared)
	add(shared)
	// The bundled catalog omits supported action pagination parameters used by the named tools.
	if strings.HasSuffix(child.Endpoint, "/actions") {
		for _, k := range []string{"before", "since", "fields"} {
			allowed[k] = map[string]any{"schema": map[string]any{"type": "string"}}
		}
		allowed["limit"] = map[string]any{"schema": map[string]any{"type": "integer", "minimum": float64(1), "maximum": float64(1000)}}
	}
	for k, p := range allowed {
		if p["required"] == true && q.Get(k) == "" {
			return bad()
		}
	}
	for k, vs := range q {
		p, ok := allowed[k]
		if !ok {
			return apiWriteInput{}, "", fmt.Errorf("Unsupported batch query parameter %s", k)
		}
		schema, _ := p["schema"].(map[string]any)
		value := vs[0]
		if enum, ok := schema["enum"].([]any); ok {
			valid := false
			for _, v := range enum {
				if fmt.Sprint(v) == value {
					valid = true
				}
			}
			if !valid {
				return bad()
			}
		}
		switch schema["type"] {
		case "boolean":
			if value != "true" && value != "false" {
				return bad()
			}
		case "integer", "number":
			n, e := strconv.ParseFloat(value, 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return bad()
			}
			if schema["type"] == "integer" {
				if _, e := strconv.Atoi(value); e != nil {
					return bad()
				}
			}
			if min, ok := schema["minimum"].(float64); ok && n < min {
				return bad()
			}
			if max, ok := schema["maximum"].(float64); ok && n > max {
				return bad()
			}
		}
	}
	// Encoding query values keeps their commas distinct from the outer route delimiter.
	normalized := u.Path
	if len(q) > 0 {
		normalized += "?" + q.Encode()
	}
	return child, normalized, nil
}

func (c *TrelloClient) prepareBatch(in apiWriteInput) ([]string, error) {
	children, routes, err := parseBatch(in)
	if err != nil {
		return nil, err
	}
	scoped := *c
	scoped.scopeCache = map[string]string{} // owned by this sequential validation only
	for _, child := range children {
		if c.cfg.APIScope != "account" {
			if v, ok := child.Query["memberships"]; ok && v != "none" && v != "false" {
				return nil, fmt.Errorf("Workspace/membership expansion requires account API scope")
			}
		}
	}
	for i, child := range children {
		path, _ := resolveAPIPath(child.Endpoint, child.PathParams)
		if err := scoped.checkAPIScope(path, child); err != nil {
			return nil, fmt.Errorf("Batch item %d failed scope validation: %w", i+1, err)
		}
	}
	return routes, nil
}

// Trello returns [{"200": body}, {"404": error}, ...], even with outer HTTP 200.
func summarizeBatch(out *apiResponse, routes []string) {
	out.BatchRoutes = routes
	success := true
	items, ok := out.Data.([]any)
	if !ok || len(items) != len(routes) {
		success = false
	}
	for i := range routes {
		valid := false
		if i < len(items) {
			if item, ok := items[i].(map[string]any); ok && len(item) == 1 {
				for k := range item {
					status, e := strconv.Atoi(k)
					valid = e == nil && status >= 200 && status < 300
				}
			}
		}
		if !valid {
			success = false
			out.BatchFailedIndexes = append(out.BatchFailedIndexes, i)
		}
	}
	out.BatchComplete = &success
}
