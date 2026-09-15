package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"
)

// Opt-in, read-only, Ariel-gated equivalence test. Logs counts only.
func TestReviewLiveEquivalence(t *testing.T) {
	if os.Getenv("TRELLO_REVIEW_LIVE_TEST") != "1" {
		t.Skip("opt-in")
	}
	cfg, e := loadConfig()
	if e != nil || cfg.APIScope != "boards" || len(cfg.AllowedBoardIDs) != 1 {
		t.Fatal("sole allowed board required")
	}
	c := NewTrelloClient(cfg)
	board := cfg.AllowedBoardIDs[0]
	var labels []TrelloLabel
	if e = c.request("GET", "/boards/"+board+"/labels", map[string]string{"fields": "id,name", "limit": "1000"}, &labels); e != nil {
		t.Fatal("labels unavailable")
	}
	label := ""
	for _, l := range labels {
		if l.Name == "Ariel" {
			if label != "" {
				t.Fatal("ambiguous label")
			}
			label = l.ID
		}
	}
	if label == "" {
		t.Fatal("label missing")
	}
	// Compare minimal board discovery via individual and batch paths.
	in := apiWriteInput{Endpoint: "/boards/{id}/cards", Method: "GET", PathParams: map[string]string{"id": board}, Query: map[string]any{"fields": "id,idLabels", "filter": "all"}}
	solo, e := c.callAPI(in)
	if e != nil {
		t.Fatal("discovery failed")
	}
	bat, e := c.callAPI(batchInput(fmt.Sprintf("/boards/%s/cards?fields=id%%2CidLabels&filter=all", board)))
	if e != nil || !*bat.BatchComplete {
		t.Fatal("discovery batch failed")
	}
	if !reflect.DeepEqual(solo.Data, bat.Data.([]any)[0].(map[string]any)["200"]) {
		t.Fatal("discovery changed/differs")
	}
	lists, e := c.ListLists(board)
	if e != nil {
		t.Fatal("lists unavailable")
	}
	ids := []string{}
	for _, l := range lists {
		if l.Name != "Prospects" && l.Name != "To Qualify" {
			continue
		}
		var meta []struct {
			ID       string   `json:"id"`
			Labels   []string `json:"idLabels"`
			Closed   bool     `json:"closed"`
			Template bool     `json:"isTemplate"`
		}
		if e = c.request("GET", "/lists/"+l.ID+"/cards", map[string]string{"fields": "id,idLabels,closed,isTemplate", "filter": "open"}, &meta); e != nil {
			t.Fatal("scope discovery failed")
		}
		for _, m := range meta {
			if contains(m.Labels, label) && !m.Closed && !m.Template {
				ids = append(ids, m.ID)
			}
		}
	}
	pages := 0
	read := func(id, filter string) []TrelloAction {
		t.Helper()
		all := []TrelloAction{}
		in := actionPageInput{CardID: id, Limit: ptr(100)}
		for {
			p, e := readCardActions(c, in, filter)
			if e != nil {
				t.Fatal("read failed")
			}
			pages++
			all = append(all, p.Actions...)
			if !p.HasMore {
				return all
			}
			in.Before = ptr(p.NextBefore)
		}
	}
	oldTime, newTime := time.Duration(0), time.Duration(0)
	oldPages, newPages := 0, 0
	normalize := func(a []TrelloAction) { sort.Slice(a, func(i, j int) bool { return a[i].ID > a[j].ID }) }
	for _, id := range ids {
		start := time.Now()
		pages = 0
		a := append(read(id, "commentCard"), read(id, historyFilter)...)
		oldTime += time.Since(start)
		oldPages += pages
		start = time.Now()
		pages = 0
		b := read(id, reviewFilter)
		newTime += time.Since(start)
		newPages += pages
		normalize(a)
		normalize(b)
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Fatal("source changed or combined evidence differs; investigate before enabling")
		}
	}
	if len(ids) == 0 {
		t.Fatal("no eligible cards")
	}
	t.Logf("cards=%d separate_pages=%d combined_pages=%d separate_http=%d combined_http=%d separate_ms=%d combined_ms=%d; discovery equality verified; all action payloads equal", len(ids), oldPages, newPages, oldPages*2, newPages*2, oldTime.Milliseconds(), newTime.Milliseconds())
}
