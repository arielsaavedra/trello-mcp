package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

func batchInput(urls string) apiWriteInput {
	return apiWriteInput{Endpoint: "/batch", Method: "GET", Query: map[string]any{"urls": urls}}
}

func TestBatchRejectsInvalidRoutesBeforeNetwork(t *testing.T) {
	for _, route := range []string{"", strings.Repeat("/boards/allowed,", 10) + "/boards/allowed", "/batch?urls=/boards/allowed", "https://example.com/boards/allowed", "//example.com/boards/allowed", "/boards/allowed#x", "/boards/%61llowed", "/boards/../allowed", "/boards/allowed/", "/boards/allowed?token=secret", "/boards/allowed?%74oken=secret", "/boards/allowed?%2574oken=secret", "/boards/allowed?fields=name&fields=id", "/boards/allowed?fields=name;token=secret", "/boards/allowed?url=https%3A%2F%2Fevil.invalid", "/boards/allowed?unknown=yes", "/boards/allowed,/batch", "/boards/allowed?memberships=true", "/cards/card1/actions?limit=1001", "/cards/card1/actions?limit=1.5", "/boards/allowed?customFields=oops", "/boards/allowed?fields=name,/members/me"} {
		t.Run(route, func(t *testing.T) {
			calls := 0
			c := fakeClient(t, func(r *http.Request) (int, string) { calls++; return 200, `{}` })
			if _, err := c.callAPI(batchInput(route)); err == nil {
				t.Fatal("invalid batch accepted")
			}
			if calls != 0 {
				t.Fatalf("invalid route reached network: %d", calls)
			}
		})
	}
}

func TestBatchCardScopeMemoIsLocalAndQueriesPreserved(t *testing.T) {
	probes, batches := 0, 0
	c := fakeClient(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/1/cards/card1":
			probes++
			if r.URL.Query().Get("fields") != "idBoard" {
				t.Fatal("probe was not metadata only")
			}
			return 200, `{"idBoard":"allowed"}`
		case "/1/batch":
			batches++
			if r.URL.Query().Get("urls") != "/cards/card1?fields=id%2Cname,/cards/card1/actions?filter=commentCard&limit=10" {
				t.Fatal("query changed", r.URL.Query().Get("urls"))
			}
			return 200, `[{"200":{"id":"card1","name":"Example"}},{"200":[]}]`
		default:
			t.Fatal("unexpected request", r.URL.Path)
			return 500, `{}`
		}
	})
	for i := 0; i < 2; i++ {
		out, err := c.callAPI(batchInput("/cards/card1?fields=id%2Cname,/cards/card1/actions?filter=commentCard&limit=10"))
		if err != nil {
			t.Fatal(err)
		}
		if out.BatchComplete == nil || !*out.BatchComplete {
			t.Fatal("valid batch incomplete")
		}
	}
	if probes != 2 || batches != 2 {
		t.Fatalf("want one fresh probe per batch: probes=%d batches=%d", probes, batches)
	}
}

func TestBatchRejectsForeignBeforeContent(t *testing.T) {
	for _, route := range []string{"/boards/allowed,/boards/foreign", "/cards/card1,/cards/foreign", "/boards/allowed,/members/me", "/boards/allowed/organization"} {
		t.Run(route, func(t *testing.T) {
			probes := 0
			c := fakeClient(t, func(r *http.Request) (int, string) {
				if r.URL.Path == "/1/batch" {
					t.Fatal("rejected batch sent")
				}
				if r.URL.Query().Get("fields") != "idBoard" {
					t.Fatal("content fetched")
				}
				probes++
				if strings.Contains(r.URL.Path, "foreign") {
					return 200, `{"idBoard":"foreign"}`
				}
				return 200, `{"idBoard":"allowed"}`
			})
			if _, err := c.callAPI(batchInput(route)); err == nil {
				t.Fatal("foreign batch accepted")
			}
			if probes > 2 {
				t.Fatal("extra probes")
			}
		})
	}
}

func TestBatchLimitsPartialAndMalformedResults(t *testing.T) {
	for _, tc := range []struct {
		body     string
		complete bool
		failed   []int
	}{
		{`[{"200":{"id":"a"}},{"404":{"message":"missing"}}]`, false, []int{1}},
		{`[{"200":[]} ]`, false, []int{1}},
		{`{"message":"success"}`, false, []int{0, 1}},
		{`[{"200":[]},{"200":[]},{"200":[]}]`, false, nil},
		{`[{"200":[],"500":{}},{"200":[]}]`, false, []int{0}},
		{`[{"200":[]},{"200":[]}]`, true, nil},
	} {
		c := fakeClient(t, func(r *http.Request) (int, string) { return 200, tc.body })
		out, err := c.callAPI(batchInput("/boards/allowed?fields=id,/boards/allowed?fields=name"))
		if err != nil {
			t.Fatal(err)
		}
		if *out.BatchComplete != tc.complete || !reflect.DeepEqual(out.BatchFailedIndexes, tc.failed) {
			t.Fatalf("wrong summary %+v", out)
		}
		var original any
		_ = json.Unmarshal([]byte(tc.body), &original)
		if !reflect.DeepEqual(original, out.Data) {
			t.Fatal("raw result changed")
		}
	}
	c := fakeClient(t, func(r *http.Request) (int, string) {
		return 200, "[" + strings.TrimSuffix(strings.Repeat(`{"200":[]},`, 10), ",") + "]"
	})
	out, err := c.callAPI(batchInput(strings.TrimSuffix(strings.Repeat("/boards/allowed?fields=id,", 10), ",")))
	if err != nil || !*out.BatchComplete {
		t.Fatal("10 routes rejected", err)
	}
}

func TestBatchAccountStillValidatesNestedURLs(t *testing.T) {
	c := fakeClient(t, func(r *http.Request) (int, string) { return 200, `[{"200":{}}]` })
	c.cfg.APIScope = "account"
	if _, err := c.callAPI(batchInput("/members/me?fields=id")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.callAPI(batchInput("/members/me?token=override")); err == nil {
		t.Fatal("nested credential accepted")
	}
}

// Opt-in metadata-only integration check. It never prints credentials or response content.
func TestBatchLiveComparison(t *testing.T) {
	if os.Getenv("TRELLO_BATCH_LIVE_TEST") != "1" {
		t.Skip("opt-in live metadata comparison")
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	if cfg.APIScope != "boards" || len(cfg.AllowedBoardIDs) != 1 {
		t.Fatal("requires exactly one authorized board")
	}
	c := NewTrelloClient(cfg)
	id := cfg.AllowedBoardIDs[0]
	expected := []any{}
	for _, field := range []string{"id", "name", "id,name"} {
		out, err := c.callAPI(apiWriteInput{Endpoint: "/boards/{id}", Method: "GET", PathParams: map[string]string{"id": id}, Query: map[string]any{"fields": field}})
		if err != nil {
			t.Fatal("individual metadata read failed")
		}
		expected = append(expected, out.Data)
	}
	out, err := c.callAPI(batchInput(fmt.Sprintf("/boards/%s?fields=id,/boards/%s?fields=name,/boards/%s?fields=id%%2Cname", id, id, id)))
	if err != nil {
		t.Fatalf("batch metadata read failed: %v", err)
	}
	if out.BatchComplete == nil || !*out.BatchComplete {
		t.Fatal("batch incomplete")
	}
	for i, item := range out.Data.([]any) {
		if !reflect.DeepEqual(item.(map[string]any)["200"], expected[i]) {
			t.Fatal("individual and batch metadata differ")
		}
	}
	t.Log("Three individual metadata reads equal one batch (including encoded field commas); no remote mutations")
}
