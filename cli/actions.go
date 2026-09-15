package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Retain Trello's action data without flattening away listBefore/listAfter,
// old values, comment text, or copy/board-transfer evidence.
type TrelloAction struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Date            string          `json:"date"`
	IDMemberCreator string          `json:"idMemberCreator"`
	MemberCreator   *TrelloMember   `json:"memberCreator,omitempty"`
	Data            json.RawMessage `json:"data"`
}

type actionPageInput struct {
	CardID string  `json:"card_id" jsonschema:"Trello card id or shortlink"`
	Limit  *int    `json:"limit,omitempty" jsonschema:"Page size 1-1000; default 100"`
	Before *string `json:"before,omitempty" jsonschema:"Exclusive older-page cursor: action id or RFC3339 timestamp"`
	Since  *string `json:"since,omitempty" jsonschema:"Only actions newer than this action id or RFC3339 timestamp; omit for full history"`
}

type ActionPage struct {
	Actions    []TrelloAction `json:"actions"`
	HasMore    bool           `json:"has_more"`
	NextBefore string         `json:"next_before,omitempty"`
}

var mongoID = regexp.MustCompile(`^[a-fA-F0-9]{24}$`)
var cardReference = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

func actionQuery(in actionPageInput, filter string) (map[string]string, error) {
	if !cardReference.MatchString(in.CardID) {
		return nil, fmt.Errorf("card_id must be an id or shortlink, not a URL")
	}
	limit := 100
	if in.Limit != nil {
		limit = *in.Limit
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("limit must be between 1 and 1000")
	}
	q := map[string]string{"filter": filter, "limit": strconv.Itoa(limit), "memberCreator": "true", "memberCreator_fields": "id,username,fullName"}
	for key, value := range map[string]*string{"before": in.Before, "since": in.Since} {
		if value == nil {
			continue
		}
		if !mongoID.MatchString(*value) {
			if _, err := time.Parse(time.RFC3339Nano, *value); err != nil {
				return nil, fmt.Errorf("%s must be an action id or RFC3339 timestamp", key)
			}
		}
		q[key] = *value
	}
	return q, nil
}

func readCardActions(client *TrelloClient, in actionPageInput, filter string) (*ActionPage, error) {
	q, err := actionQuery(in, filter)
	if err != nil {
		return nil, err
	}
	if err := ensureOnboardingComplete(client.cfg); err != nil {
		return nil, err
	}
	if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
		return nil, err
	}
	actions := []TrelloAction{}
	if err := client.request(http.MethodGet, "/cards/"+in.CardID+"/actions", q, &actions); err != nil {
		return nil, err
	}
	limit, _ := strconv.Atoi(q["limit"])
	page := &ActionPage{Actions: actions, HasMore: len(actions) == limit}
	// A full page may be the last page. Do not claim completion until a shorter
	// (possibly empty) page is returned. Use IDs, not dates, for stable paging.
	if page.HasMore {
		page.NextBefore = actions[len(actions)-1].ID
	}
	return page, nil
}

const historyFilter = "createCard,copyCard,updateCard:idList,moveCardToBoard,moveCardFromBoard"
const reviewFilter = "commentCard," + historyFilter

func registerHistoryTools(server *mcp.Server, client *TrelloClient) {
	for _, spec := range []struct{ name, description, filter string }{
		{"list_card_comments", "Read existing comments with author, timestamp and text, newest first. Follow next_before while has_more; a page is not the complete conversation.", "commentCard"},
		{"list_card_history", "Read card creation, copy, list and board movement history, newest first. Follow next_before while has_more; omit since when establishing uninterrupted time in a list.", historyFilter},
		{"list_card_review_history", "Read comments plus creation/copy/list/board transitions in one paginated stream. Preserves authors and full action data. Follow next_before until has_more is false; omit since to establish list entry. Does not cover unrelated action types.", reviewFilter},
	} {
		mcp.AddTool(server, &mcp.Tool{Name: spec.name, Description: spec.description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, _ *mcp.CallToolRequest, in actionPageInput) (*mcp.CallToolResult, any, error) {
			page, err := readCardActions(client, in, spec.filter)
			if err != nil {
				return nil, nil, err
			}
			return jsonResult(page)
		})
	}
	mcp.AddTool(server, &mcp.Tool{Name: "list_board_members", Description: "Read allowed-board members to resolve real usernames for mentions, including members not assigned to a card", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, _ *mcp.CallToolRequest, in listListsInput) (*mcp.CallToolResult, any, error) {
		if err := ensureBoardAccess(client.cfg, in.BoardID); err != nil {
			return nil, nil, err
		}
		members := []TrelloMember{}
		if err := client.request(http.MethodGet, "/boards/"+in.BoardID+"/members", map[string]string{"fields": "id,username,fullName"}, &members); err != nil {
			return nil, nil, err
		}
		return jsonResult(members)
	})
}
