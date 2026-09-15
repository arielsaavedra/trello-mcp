package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type copyTemplateInput struct {
	BoardID        string `json:"board_id" jsonschema:"Allowed destination board id"`
	ListID         string `json:"list_id" jsonschema:"Destination list on that board"`
	TemplateCardID string `json:"template_card_id" jsonschema:"Verified template card on the same board"`
	Name           string `json:"name" jsonschema:"New card title"`
}

func copyTemplateCard(client *TrelloClient, in copyTemplateInput) (*TrelloCard, error) {
	if err := ensureBoardAccess(client.cfg, in.BoardID); err != nil {
		return nil, err
	}
	if !cardReference.MatchString(in.TemplateCardID) || strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("A template card id and non-empty title are required")
	}
	template, err := client.GetCard(in.TemplateCardID)
	if err != nil {
		return nil, err
	}
	// Trello hides reusable templates by archiving them; closed does not mean
	// the template is unavailable. Never unarchive or mutate the source.
	if template.IDBoard != in.BoardID || !template.IsTemplate {
		return nil, fmt.Errorf("Source must be a template card on the destination board")
	}
	lists, err := client.ListLists(in.BoardID)
	if err != nil {
		return nil, err
	}
	if !hasListID(lists, in.ListID) {
		return nil, fmt.Errorf("Destination list not found on the allowed board")
	}
	labelIDs := make([]string, 0, len(template.Labels))
	for _, label := range template.Labels {
		labelIDs = append(labelIDs, label.ID)
	}
	var card TrelloCard
	err = client.request(http.MethodPost, "/cards", map[string]string{
		"idList": in.ListID, "name": in.Name, "idCardSource": template.ID,
		"keepFromSource": "checklists", "desc": template.Desc,
		"idLabels": strings.Join(labelIDs, ","), "pos": "bottom",
	}, &card)
	return &card, err
}

func registerTemplateTool(server *mcp.Server, client *TrelloClient) {
	mcp.AddTool(server, &mcp.Tool{Name: "copy_template_card", Description: "Create a card from a verified template on the same allowed board, including hidden/archived templates, preserving its description, checklists and labels at creation. Does not change the source or copy comments, members, attachments or due dates. Requires write approval."}, func(_ context.Context, _ *mcp.CallToolRequest, in copyTemplateInput) (*mcp.CallToolResult, any, error) {
		card, err := copyTemplateCard(client, in)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(card)
	})
}
