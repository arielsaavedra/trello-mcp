package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeClient(t *testing.T, handler func(*http.Request) (int, string)) *TrelloClient {
	t.Helper()
	c := NewTrelloClient(&AppConfig{APIKey: "test-key", Token: "test-token", AllowedBoardIDs: []string{"allowed"}})
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status, body := handler(r)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	return c
}
func ptr[T any](v T) *T { return &v }

func TestCommentsPagingPreservesEvidence(t *testing.T) {
	requests := 0
	c := fakeClient(t, func(r *http.Request) (int, string) {
		requests++
		if r.Method != "GET" {
			t.Fatalf("unexpected write: %s", r.Method)
		}
		if r.URL.Path == "/1/cards/card1" {
			return 200, `{"id":"card1","idBoard":"allowed"}`
		}
		q := r.URL.Query()
		if q.Get("filter") != "commentCard" || q.Get("limit") != "1" || q.Get("memberCreator") != "true" {
			t.Fatalf("wrong query %v", q)
		}
		if q.Get("before") != "" {
			return 200, `[]`
		}
		return 200, `[{"id":"111111111111111111111111","type":"commentCard","date":"2026-01-02T12:00:00Z","idMemberCreator":"member1","memberCreator":{"id":"member1","username":"caseworker","fullName":"Case Worker"},"data":{"text":"Documents requested","card":{"id":"card1"}}}]`
	})
	p, err := readCardActions(c, actionPageInput{CardID: "card1", Limit: ptr(1)}, "commentCard")
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasMore || p.NextBefore != "111111111111111111111111" || p.Actions[0].MemberCreator.Username != "caseworker" || !strings.Contains(string(p.Actions[0].Data), "Documents requested") {
		t.Fatalf("lost evidence: %+v", p)
	}
	p, err = readCardActions(c, actionPageInput{CardID: "card1", Limit: ptr(1), Before: ptr(p.NextBefore)}, "commentCard")
	if err != nil || p.HasMore || p.NextBefore != "" || len(p.Actions) != 0 {
		t.Fatalf("bad final page %+v %v", p, err)
	}
	if requests != 4 {
		t.Fatalf("unexpected request count %d", requests)
	}
}

func TestHistoryFiltersAndKeepsListTransitions(t *testing.T) {
	filter := "createCard,copyCard,updateCard:idList,moveCardToBoard,moveCardFromBoard"
	c := fakeClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/1/cards/card1" {
			return 200, `{"idBoard":"allowed"}`
		}
		if r.URL.Query().Get("filter") != filter || r.URL.Query().Get("since") != "2026-01-01T00:00:00Z" {
			t.Fatal("filter/cursor not forwarded")
		}
		return 200, `[{"id":"222222222222222222222222","type":"updateCard","date":"2026-01-02T00:00:00Z","data":{"listBefore":{"id":"old"},"listAfter":{"id":"new"},"old":{"idList":"old"}}}]`
	})
	p, err := readCardActions(c, actionPageInput{CardID: "card1", Since: ptr("2026-01-01T00:00:00Z")}, filter)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(p.Actions[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["listBefore"] == nil || data["listAfter"] == nil || data["old"] == nil || p.HasMore {
		t.Fatal("lost history evidence")
	}
}

func TestActionsRejectScopeAndInvalidInputs(t *testing.T) {
	for _, in := range []actionPageInput{{CardID: "card1", Limit: ptr(0)}, {CardID: "card1", Limit: ptr(1001)}, {CardID: "card1", Before: ptr("bad")}, {CardID: "../other"}} {
		c := fakeClient(t, func(r *http.Request) (int, string) { t.Fatal("invalid input reached network"); return 500, "" })
		if _, err := readCardActions(c, in, "commentCard"); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	c := fakeClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/1/cards/card1" {
			t.Fatal("out-of-scope actions requested")
		}
		return 200, `{"idBoard":"other"}`
	})
	if _, err := readCardActions(c, actionPageInput{CardID: "card1"}, "commentCard"); err == nil {
		t.Fatal("out-of-scope card accepted")
	}
	c.cfg.OnboardingRequired = true
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("incomplete onboarding reached network")
		return nil, nil
	})
	if _, err := readCardActions(c, actionPageInput{CardID: "card1"}, "commentCard"); err == nil {
		t.Fatal("onboarding ignored")
	}
}

func TestActionFailureIsNotEmptyHistory(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		c := fakeClient(t, func(r *http.Request) (int, string) {
			if r.URL.Path == "/1/cards/card1" {
				return 200, `{"idBoard":"allowed"}`
			}
			return status, `{"message":"request failed"}`
		})
		p, err := readCardActions(c, actionPageInput{CardID: "card1"}, "commentCard")
		if err == nil || p != nil {
			t.Fatalf("status %d became empty success", status)
		}
	}
}

func TestListScopeAndArchiveFilter(t *testing.T) {
	c := fakeClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/1/boards/allowed/lists" {
			return 200, `[{"id":"list1"}]`
		}
		if r.URL.Path != "/1/lists/list1/cards" || r.URL.Query().Get("filter") != "all" {
			t.Fatal("wrong list scope/filter")
		}
		return 200, `[{"id":"archived","closed":true,"isTemplate":true,"pos":1}]`
	})
	if _, err := c.ListCards("allowed", ptr("foreign"), true); err == nil {
		t.Fatal("foreign list accepted")
	}
	cards, err := c.ListCards("allowed", ptr("list1"), true)
	if err != nil || len(cards) != 1 || !cards[0].Closed || !cards[0].IsTemplate {
		t.Fatal("archive/template fields lost", err)
	}
}

func TestTemplateCopyAndMove(t *testing.T) {
	writes := 0
	c := fakeClient(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/1/cards/template":
			return 200, `{"id":"template","idBoard":"allowed","isTemplate":true,"closed":true,"desc":"Keep this template"}`
		case "/1/boards/allowed/lists":
			return 200, `[{"id":"list1"}]`
		case "/1/cards":
			q := r.URL.Query()
			if r.Method != "POST" || q.Get("idCardSource") != "template" || q.Get("desc") != "Keep this template" || q.Get("keepFromSource") != "checklists" {
				t.Fatal("template copy did not preserve intended source")
			}
			writes++
			return 200, `{"id":"new"}`
		case "/1/cards/new":
			if r.Method != "PUT" || r.URL.Query().Get("pos") != "top" || r.URL.Query().Get("idList") != "list1" {
				t.Fatal("position lost")
			}
			writes++
			return 200, `{"id":"new","idList":"list1","pos":1}`
		}
		t.Fatal("unexpected request", r.URL.Path)
		return 500, ""
	})
	if _, err := copyTemplateCard(c, copyTemplateInput{BoardID: "allowed", ListID: "list1", TemplateCardID: "template", Name: "New applicant"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MoveCard("new", "list1", ptr("top")); err != nil {
		t.Fatal(err)
	}
	if writes != 2 {
		t.Fatal("unexpected writes", writes)
	}
	if _, err := copyTemplateCard(c, copyTemplateInput{BoardID: "foreign"}); err == nil {
		t.Fatal("foreign board accepted")
	}
}

func TestTransportErrorsDoNotExposeCredentials(t *testing.T) {
	u := &url.Error{Op: "Get", URL: "https://api.trello.com/1/cards/card?key=test-key&token=test-token", Err: fmt.Errorf("connection failed")}
	if strings.Contains(safeTransportError(u).Error(), "test-") {
		t.Fatal("credential leak")
	}
}

func TestUpdateChecklistItemUsesPublishedCardEndpoint(t *testing.T) {
	c := fakeClient(t, func(r *http.Request) (int, string) {
		if r.Method == "GET" && r.URL.Path == "/1/checklists/checklist1" {
			return 200, `{"id":"checklist1","idCard":"card1"}`
		}
		if r.Method != "PUT" || r.URL.Path != "/1/cards/card1/checkItem/item1" {
			t.Fatal("Wrong checklist update endpoint", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("state") != "complete" {
			t.Fatal("Milestone state not encoded correctly")
		}
		return 200, `{"id":"item1","state":"complete"}`
	})
	item, err := c.UpdateCheckItem("checklist1", "item1", UpdateCheckItemInput{Checked: ptr(true)})
	if err != nil || item.State != "complete" {
		t.Fatal("Checklist update failed", err)
	}
}
