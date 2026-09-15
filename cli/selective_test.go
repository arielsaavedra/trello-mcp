package main

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestSelectiveSnapshotAndMetadataProbe(t *testing.T) {
	for _, attach := range []bool{true, false} {
		for _, checks := range []bool{true, false} {
			calls := 0
			c := fakeClient(t, func(r *http.Request) (int, string) {
				calls++
				if r.URL.Path != "/1/cards/card1" {
					t.Fatal("unexpected path")
				}
				q := r.URL.Query()
				if calls == 1 {
					if q.Get("fields") != "idBoard" {
						t.Fatal("scope probe leaked content")
					}
					return 200, `{"idBoard":"allowed"}`
				}
				if q.Get("attachments") != boolStr(attach) {
					t.Fatal("attachment projection mismatch")
				}
				if (q.Get("checklists") == "all") != checks {
					t.Fatal("checklist projection mismatch")
				}
				return 200, `{"id":"card1","idBoard":"allowed","desc":"scenario","attachments":[],"checklists":[]}`
			})
			if _, err := ensureCardBoardAccess(c, "card1"); err != nil {
				t.Fatal(err)
			}
			out, err := c.GetCardSnapshot("card1", attach, checks)
			if err != nil {
				t.Fatal(err)
			}
			if contains(out.Omitted, "attachments") == attach || contains(out.Omitted, "checklists") == checks {
				t.Fatal("omitted data must be explicit")
			}
			if calls != 2 {
				t.Fatal("want scope+combined content only", calls)
			}
		}
	}
}

func TestForeignSelectiveCardStopsBeforeContent(t *testing.T) {
	calls := 0
	c := fakeClient(t, func(r *http.Request) (int, string) {
		calls++
		if r.URL.Query().Get("fields") != "idBoard" {
			t.Fatal("foreign content read")
		}
		return 200, `{"idBoard":"foreign"}`
	})
	if _, err := ensureCardBoardAccess(c, "card1"); err == nil {
		t.Fatal("foreign accepted")
	}
	if calls != 1 {
		t.Fatal("extra reads")
	}
}

// Read-only input benchmark: all active Ariel cards in Prospects/To Qualify.
// Does not claim to benchmark Outlook, CRM, model reasoning or full Planning time.
func TestSelectiveLiveEquivalence(t *testing.T) {
	if os.Getenv("TRELLO_SELECTIVE_LIVE_TEST") != "1" {
		t.Skip("opt-in read-only comparison")
	}
	cfg, err := loadConfig()
	if err != nil || cfg.APIScope != "boards" || len(cfg.AllowedBoardIDs) != 1 {
		t.Fatal("requires configured sole board")
	}
	c := NewTrelloClient(cfg)
	board := cfg.AllowedBoardIDs[0]
	var labels []TrelloLabel
	if err := c.request("GET", "/boards/"+board+"/labels", map[string]string{"fields": "id,name", "limit": "1000"}, &labels); err != nil {
		t.Fatal("label inventory failed")
	}
	label := ""
	for _, l := range labels {
		if l.Name == "Ariel" {
			if label != "" {
				t.Fatal("ambiguous Ariel")
			}
			label = l.ID
		}
	}
	if label == "" {
		t.Fatal("Ariel unavailable")
	}
	lists, err := c.ListLists(board)
	if err != nil {
		t.Fatal("lists unavailable")
	}
	scoped := map[string]bool{}
	for _, l := range lists {
		if l.Name == "Prospects" || l.Name == "To Qualify" || l.Name == "To-Qualify" {
			scoped[l.ID] = true
		}
	}
	if len(scoped) != 2 {
		t.Fatal("scope lists unavailable")
	}
	var metadata []struct {
		ID     string   `json:"id"`
		Labels []string `json:"idLabels"`
	}
	if err := c.request("GET", "/boards/"+board+"/cards", map[string]string{"fields": "id,idLabels", "filter": "all", "limit": "1000"}, &metadata); err != nil || len(metadata) >= 1000 {
		t.Fatal("inventory incomplete")
	}
	count, changed := 0, 0
	oldTime, newTime := time.Duration(0), time.Duration(0)
	oldBytes, newBytes := 0, 0
	for _, m := range metadata {
		if !contains(m.Labels, label) {
			continue
		}
		// Eligibility before content. Fresh metadata card read selects the required lists.
		card, err := c.GetCard(m.ID)
		if err != nil {
			t.Fatal("card unavailable")
		}
		if card.Closed || card.IsTemplate || !scoped[card.IDList] {
			continue
		}
		start := time.Now()
		// Reproduce the former get_card HTTP pattern: scope using full card, full card again, attachments, checklists.
		if _, err := c.GetCard(m.ID); err != nil {
			t.Fatal(err)
		}
		base, err := c.GetCard(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		attachments, err := c.ListAttachments(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		checklists, err := c.ListChecklists(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		oldTime += time.Since(start)
		expected := &CardWithAttachments{TrelloCard: *base, Attachments: attachments, Checklists: checklists}
		start = time.Now()
		if _, err := ensureCardBoardAccess(c, m.ID); err != nil {
			t.Fatal(err)
		}
		got, err := c.GetCardSnapshot(m.ID, true, true)
		if err != nil {
			t.Fatal(err)
		}
		newTime += time.Since(start)
		if !reflect.DeepEqual(expected, got) {
			changed++
			t.Log("Difference requires investigation; no borrower content logged")
		}
		a, _ := json.Marshal(expected)
		b, _ := json.Marshal(got)
		oldBytes += len(a)
		newBytes += len(b)
		count++
	}
	t.Logf("cards=%d former_http=%d combined_http=%d former_ms=%d combined_ms=%d former_result_bytes=%d combined_result_bytes=%d unequal=%d", count, count*4, count*2, oldTime.Milliseconds(), newTime.Milliseconds(), oldBytes, newBytes, changed)
	if count == 0 || changed != 0 {
		t.Fatal("equivalence not established")
	}
}
