package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type emptyInput struct{}

type listListsInput struct {
	BoardID string `json:"board_id" jsonschema:"Trello board id"`
}

type listCardsInput struct {
	BoardID       string  `json:"board_id" jsonschema:"Trello board id"`
	ListID        *string `json:"list_id,omitempty" jsonschema:"Optional Trello list id"`
	IncludeClosed bool    `json:"include_closed,omitempty" jsonschema:"Include archived cards for duplicate checks; default false"`
}

type cardIDInput struct {
	CardID string `json:"card_id" jsonschema:"Trello card id"`
}

type createCardInput struct {
	BoardID  string   `json:"board_id" jsonschema:"Trello board id"`
	ListID   string   `json:"list_id" jsonschema:"Trello list id"`
	Name     string   `json:"name" jsonschema:"Card title"`
	Desc     *string  `json:"desc,omitempty" jsonschema:"Card description (markdown supported)"`
	Due      *string  `json:"due,omitempty" jsonschema:"Due date in ISO 8601 format"`
	LabelIDs []string `json:"label_ids,omitempty" jsonschema:"Trello label ids"`
}

type updateCardInput struct {
	CardID      string   `json:"card_id" jsonschema:"Trello card id"`
	Name        *string  `json:"name,omitempty" jsonschema:"New card title"`
	Desc        *string  `json:"desc,omitempty" jsonschema:"New card description"`
	Due         *string  `json:"due,omitempty" jsonschema:"Due date in ISO 8601 format, or empty string to clear"`
	DueComplete *bool    `json:"due_complete,omitempty" jsonschema:"Mark due date complete"`
	Closed      *bool    `json:"closed,omitempty" jsonschema:"Archive the card when true"`
	LabelIDs    []string `json:"label_ids,omitempty" jsonschema:"Replace card labels with these ids"`
}

type moveCardInput struct {
	CardID   string  `json:"card_id" jsonschema:"Trello card id"`
	ListID   string  `json:"list_id" jsonschema:"Destination list id"`
	Position *string `json:"position,omitempty" jsonschema:"Optional top or bottom position in the destination list"`
}

type addCommentInput struct {
	CardID string `json:"card_id" jsonschema:"Trello card id"`
	Text   string `json:"text" jsonschema:"Comment body"`
}

type addAttachmentInput struct {
	CardID   string  `json:"card_id" jsonschema:"Trello card id"`
	URL      *string `json:"url,omitempty" jsonschema:"Public URL for Trello to fetch and attach"`
	FilePath *string `json:"file_path,omitempty" jsonschema:"Absolute local file path to upload"`
	Name     *string `json:"name,omitempty" jsonschema:"Attachment display name"`
	MimeType *string `json:"mime_type,omitempty" jsonschema:"MIME type when uploading a local file"`
	SetCover *bool   `json:"set_cover,omitempty" jsonschema:"Use attachment as card cover when supported"`
}

type addChecklistInput struct {
	CardID string `json:"card_id" jsonschema:"Trello card id"`
	Name   string `json:"name" jsonschema:"Checklist title"`
}

type checklistIDInput struct {
	ChecklistID string `json:"checklist_id" jsonschema:"Trello checklist id"`
}

type addCheckItemInput struct {
	ChecklistID string `json:"checklist_id" jsonschema:"Trello checklist id"`
	Name        string `json:"name" jsonschema:"Check item text"`
	Checked     *bool  `json:"checked,omitempty" jsonschema:"Mark item complete on creation"`
}

type updateCheckItemInput struct {
	ChecklistID string  `json:"checklist_id" jsonschema:"Trello checklist id"`
	CheckItemID string  `json:"check_item_id" jsonschema:"Trello check item id"`
	Name        *string `json:"name,omitempty" jsonschema:"New check item text"`
	Checked     *bool   `json:"checked,omitempty" jsonschema:"Mark item complete or incomplete"`
	Due         *string `json:"due,omitempty" jsonschema:"Due date in ISO 8601 format, or empty string to clear"`
}

type deleteCheckItemInput struct {
	ChecklistID string `json:"checklist_id" jsonschema:"Trello checklist id"`
	CheckItemID string `json:"check_item_id" jsonschema:"Trello check item id"`
}

type listAvailableBoardsInput struct {
	IncludeClosed *bool `json:"include_closed,omitempty" jsonschema:"Include archived boards (default: false)"`
}

type selectAllowedBoardsInput struct {
	BoardIDs []string `json:"board_ids" jsonschema:"One or more Trello board ids selected by the user"`
}

func registerTools(server *mcp.Server, client *TrelloClient) {
	registerOnboardingTools(server, client)
	registerHistoryTools(server, client)
	registerTemplateTool(server, client)
	registerAPITools(server, client)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_boards",
		Description: "List Trello boards allowed by TRELLO_ALLOWED_BOARD_IDS",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		if client.cfg.OnboardingRequired {
			return textResult(onboardingRequiredMessage())
		}

		boards, err := client.ListBoards()
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(boards)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_lists",
		Description: "List lists (columns) on an allowed board",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listListsInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if err := ensureBoardAccess(client.cfg, in.BoardID); err != nil {
			return nil, nil, err
		}

		lists, err := client.ListLists(in.BoardID)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(lists)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_cards",
		Description: "List cards on an allowed board, optionally filtered by list",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listCardsInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if err := ensureBoardAccess(client.cfg, in.BoardID); err != nil {
			return nil, nil, err
		}

		cards, err := client.ListCards(in.BoardID, in.ListID, in.IncludeClosed)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(cards)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_card",
		Description: "Get a Trello card by id, including attachments and checklists",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in cardIDInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		card, err := client.GetCardWithAttachments(in.CardID)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(card)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_attachments",
		Description: "List attachments on an allowed-board card (URLs, files, GitHub PR links)",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in cardIDInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		attachments, err := client.ListAttachments(in.CardID)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(attachments)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_card_members",
		Description: "List the members assigned to an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in cardIDInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		members, err := client.ListCardMembers(in.CardID)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(members)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_card",
		Description: "Create a card on an allowed board list",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in createCardInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if err := ensureBoardAccess(client.cfg, in.BoardID); err != nil {
			return nil, nil, err
		}

		lists, err := client.ListLists(in.BoardID)
		if err != nil {
			return nil, nil, err
		}

		if !hasListID(lists, in.ListID) {
			return nil, nil, fmt.Errorf("List %s was not found on board %s", in.ListID, in.BoardID)
		}

		card, err := client.CreateCard(CreateCardInput{
			IDList:   in.ListID,
			Name:     in.Name,
			Desc:     in.Desc,
			Due:      in.Due,
			IDLabels: in.LabelIDs,
		})
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(card)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_card",
		Description: "Update fields on an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in updateCardInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		card, err := client.UpdateCard(in.CardID, UpdateCardInput{
			Name:        in.Name,
			Desc:        in.Desc,
			Due:         in.Due,
			DueComplete: in.DueComplete,
			Closed:      in.Closed,
			IDLabels:    in.LabelIDs,
		})
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(card)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "move_card",
		Description: "Move a card to another list on the same board",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in moveCardInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		boardID, err := ensureCardBoardAccess(client, in.CardID)
		if err != nil {
			return nil, nil, err
		}

		lists, err := client.ListLists(boardID)
		if err != nil {
			return nil, nil, err
		}

		if !hasListID(lists, in.ListID) {
			return nil, nil, fmt.Errorf("List %s was not found on board %s", in.ListID, boardID)
		}

		card, err := client.MoveCard(in.CardID, in.ListID, in.Position)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(card)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_comment",
		Description: "Add a comment to an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in addCommentInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		comment, err := client.AddComment(in.CardID, in.Text)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(comment)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "archive_card",
		Description: "Archive (close) an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in cardIDInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		card, err := client.ArchiveCard(in.CardID)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(card)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_attachment",
		Description: "Attach a file URL or local file path to an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in addAttachmentInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		hasURL := in.URL != nil && *in.URL != ""
		hasFile := in.FilePath != nil && *in.FilePath != ""

		if !hasURL && !hasFile {
			return nil, nil, fmt.Errorf("Provide either url or file_path")
		}

		if hasURL && hasFile {
			return nil, nil, fmt.Errorf("Provide only one of url or file_path")
		}

		attachment, err := client.AddAttachment(AddAttachmentInput{
			CardID:   in.CardID,
			URL:      in.URL,
			FilePath: in.FilePath,
			Name:     in.Name,
			MimeType: in.MimeType,
			SetCover: in.SetCover,
		})
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(attachment)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_checklists",
		Description: "List checklists and items on an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in cardIDInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		checklists, err := client.ListChecklists(in.CardID)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(checklists)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_checklist",
		Description: "Add a checklist to an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in addChecklistInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureCardBoardAccess(client, in.CardID); err != nil {
			return nil, nil, err
		}

		checklist, err := client.AddChecklist(in.CardID, in.Name)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(checklist)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_checklist",
		Description: "Delete a checklist from an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in checklistIDInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureChecklistBoardAccess(client, in.ChecklistID); err != nil {
			return nil, nil, err
		}

		if err := client.DeleteChecklist(in.ChecklistID); err != nil {
			return nil, nil, err
		}

		return jsonResult(map[string]any{
			"deleted":      true,
			"checklist_id": in.ChecklistID,
		})
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_check_item",
		Description: "Add an item to a checklist on an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in addCheckItemInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureChecklistBoardAccess(client, in.ChecklistID); err != nil {
			return nil, nil, err
		}

		item, err := client.AddCheckItem(in.ChecklistID, in.Name, in.Checked)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(item)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_check_item",
		Description: "Update a checklist item on an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in updateCheckItemInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureChecklistBoardAccess(client, in.ChecklistID); err != nil {
			return nil, nil, err
		}

		item, err := client.UpdateCheckItem(in.ChecklistID, in.CheckItemID, UpdateCheckItemInput{
			Name:    in.Name,
			Checked: in.Checked,
			Due:     in.Due,
		})
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(item)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_check_item",
		Description: "Delete an item from a checklist on an allowed-board card",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in deleteCheckItemInput) (*mcp.CallToolResult, any, error) {
		if err := ensureOnboardingComplete(client.cfg); err != nil {
			return nil, nil, err
		}

		if _, err := ensureChecklistBoardAccess(client, in.ChecklistID); err != nil {
			return nil, nil, err
		}

		if err := client.DeleteCheckItem(in.ChecklistID, in.CheckItemID); err != nil {
			return nil, nil, err
		}

		return jsonResult(map[string]any{
			"deleted":       true,
			"checklist_id":  in.ChecklistID,
			"check_item_id": in.CheckItemID,
		})
	})
}

func registerOnboardingTools(server *mcp.Server, client *TrelloClient) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_setup_status",
		Description: "Check whether Trello board onboarding is required before using other tools",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		if client.cfg.OnboardingRequired {
			return jsonResult(map[string]any{
				"onboarding_required": true,
				"message":             "No boards configured. Call list_available_boards, ask the user which boards to use, then call select_allowed_boards with the chosen board ids.",
				"config_path":         client.cfg.ConfigPath,
			})
		}

		return jsonResult(map[string]any{
			"onboarding_required": false,
			"allowed_board_ids":   client.cfg.AllowedBoardIDs,
			"config_path":         client.cfg.ConfigPath,
			"api_scope":           client.cfg.APIScope,
		})
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_available_boards",
		Description: "List all Trello boards accessible to the authenticated user (used during onboarding)",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listAvailableBoardsInput) (*mcp.CallToolResult, any, error) {
		if client.cfg.BoardIDsLocked {
			boards, err := client.ListBoards()
			if err != nil {
				return nil, nil, err
			}
			return jsonResult(boards)
		}
		boards, err := client.ListAllBoards()
		if err != nil {
			return nil, nil, err
		}

		includeClosed := in.IncludeClosed != nil && *in.IncludeClosed
		out := make([]map[string]any, 0, len(boards))

		for _, b := range boards {
			if !includeClosed && b.Closed {
				continue
			}

			out = append(out, map[string]any{
				"id":     b.ID,
				"name":   b.Name,
				"url":    b.URL,
				"closed": b.Closed,
			})
		}

		return jsonResult(out)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "select_allowed_boards",
		Description: "Save the boards the user chose during onboarding and enable the other Trello tools",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in selectAllowedBoardsInput) (*mcp.CallToolResult, any, error) {
		if client.cfg.BoardIDsLocked {
			return nil, nil, fmt.Errorf("Board selection is locked by TRELLO_ALLOWED_BOARD_IDS; change the local configuration to change scope")
		}
		unique := uniqueNonEmpty(in.BoardIDs)

		if len(unique) == 0 {
			return nil, nil, fmt.Errorf("board_ids must include at least one board id")
		}

		available, err := client.ListAllBoards()
		if err != nil {
			return nil, nil, err
		}

		availableIDs := map[string]bool{}
		for _, b := range available {
			availableIDs[b.ID] = true
		}

		var invalid []string
		for _, id := range unique {
			if !availableIDs[id] {
				invalid = append(invalid, id)
			}
		}

		if len(invalid) > 0 {
			return nil, nil, fmt.Errorf("Board(s) not found or not accessible: %s", strings.Join(invalid, ", "))
		}

		if err := saveAllowedBoardIDs(client.cfg.ConfigPath, unique); err != nil {
			return nil, nil, err
		}

		client.cfg.AllowedBoardIDs = unique
		client.cfg.OnboardingRequired = false

		selected := make([]map[string]any, 0, len(unique))
		for _, b := range available {
			if contains(unique, b.ID) {
				selected = append(selected, map[string]any{
					"id":   b.ID,
					"name": b.Name,
					"url":  b.URL,
				})
			}
		}

		return jsonResult(map[string]any{
			"message":           "Setup complete. Trello tools are now enabled for the selected boards.",
			"allowed_board_ids": unique,
			"boards":            selected,
			"persisted_to":      client.cfg.ConfigPath,
			"env_override":      "TRELLO_ALLOWED_BOARD_IDS=" + strings.Join(unique, ","),
		})
	})
}

func jsonResult(data any) (*mcp.CallToolResult, any, error) {
	text, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, nil, err
	}

	return textResult(string(text))
}

func textResult(text string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}

func ensureOnboardingComplete(cfg *AppConfig) error {
	if cfg.OnboardingRequired {
		return &TrelloMcpError{Message: onboardingRequiredMessage()}
	}

	return nil
}

func ensureBoardAccess(cfg *AppConfig, boardID string) error {
	return assertBoardAllowed(boardID, cfg)
}

func ensureCardBoardAccess(client *TrelloClient, cardID string) (string, error) {
	card, err := client.GetCard(cardID)
	if err != nil {
		return "", err
	}

	if !contains(client.cfg.AllowedBoardIDs, card.IDBoard) {
		return "", &TrelloMcpError{Message: fmt.Sprintf("Board %s is not in TRELLO_ALLOWED_BOARD_IDS", card.IDBoard)}
	}

	return card.IDBoard, nil
}

func ensureChecklistBoardAccess(client *TrelloClient, checklistID string) (string, error) {
	checklist, err := client.GetChecklist(checklistID)
	if err != nil {
		return "", err
	}

	return ensureCardBoardAccess(client, checklist.IDCard)
}

func hasListID(lists []TrelloList, listID string) bool {
	for _, list := range lists {
		if list.ID == listID {
			return true
		}
	}

	return false
}
