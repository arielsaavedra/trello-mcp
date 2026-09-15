package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const trelloAPIBase = "https://api.trello.com/1"

type TrelloBoard struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Closed bool   `json:"closed"`
}

type TrelloList struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Closed  bool   `json:"closed"`
	IDBoard string `json:"idBoard"`
}

type TrelloLabel struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

type TrelloCard struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Desc             string        `json:"desc"`
	URL              string        `json:"url"`
	Closed           bool          `json:"closed"`
	IDBoard          string        `json:"idBoard"`
	IDList           string        `json:"idList"`
	Due              *string       `json:"due"`
	DueComplete      bool          `json:"dueComplete"`
	Labels           []TrelloLabel `json:"labels"`
	IsTemplate       bool          `json:"isTemplate"`
	Pos              float64       `json:"pos"`
	DateLastActivity string        `json:"dateLastActivity"`
}

type TrelloAttachment struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	URL      string  `json:"url"`
	MimeType *string `json:"mimeType"`
	Bytes    *int64  `json:"bytes"`
	Date     string  `json:"date"`
	IsUpload bool    `json:"isUpload"`
}

type TrelloMember struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	FullName string `json:"fullName"`
}

type TrelloCheckItem struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	State string  `json:"state"`
	Due   *string `json:"due"`
	Pos   float64 `json:"pos"`
}

type TrelloChecklist struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	IDCard     string            `json:"idCard"`
	CheckItems []TrelloCheckItem `json:"checkItems"`
}

type CardWithAttachments struct {
	Omitted []string `json:"omitted,omitempty"`
	TrelloCard
	Attachments []TrelloAttachment `json:"attachments"`
	Checklists  []TrelloChecklist  `json:"checklists"`
}

type CreateCardInput struct {
	IDList   string
	Name     string
	Desc     *string
	Due      *string
	IDLabels []string
}

type UpdateCardInput struct {
	Name        *string
	Desc        *string
	Due         *string
	DueComplete *bool
	Closed      *bool
	IDLabels    []string
}

type AddAttachmentInput struct {
	CardID   string
	URL      *string
	FilePath *string
	Name     *string
	MimeType *string
	SetCover *bool
}

// TrelloAPIError mirrors the TS TrelloApiError class.
type TrelloAPIError struct {
	Status  int
	Message string
}

func (e *TrelloAPIError) Error() string {
	return fmt.Sprintf("Trello API error (%d): %s", e.Status, e.Message)
}

type TrelloClient struct {
	scopeCache map[string]string // non-nil only on a request-local batch validation copy
	cfg        *AppConfig
	http       *http.Client
}

func NewTrelloClient(cfg *AppConfig) *TrelloClient {
	return &TrelloClient{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *TrelloClient) ListAllBoards() ([]TrelloBoard, error) {
	var boards []TrelloBoard
	err := c.request(http.MethodGet, "/members/me/boards", map[string]string{
		"fields": "id,name,url,closed",
	}, &boards)

	return boards, err
}

func (c *TrelloClient) ListBoards() ([]TrelloBoard, error) {
	allowed := make([]TrelloBoard, 0, len(c.cfg.AllowedBoardIDs))
	for _, id := range c.cfg.AllowedBoardIDs {
		board, err := c.GetBoard(id)
		if err != nil {
			return nil, err
		}
		allowed = append(allowed, *board)
	}

	return allowed, nil
}

func (c *TrelloClient) GetBoard(boardID string) (*TrelloBoard, error) {
	var board TrelloBoard
	err := c.request(http.MethodGet, "/boards/"+boardID, map[string]string{
		"fields": "id,name,url,closed",
	}, &board)

	return &board, err
}

func (c *TrelloClient) ListLists(boardID string) ([]TrelloList, error) {
	var lists []TrelloList
	err := c.request(http.MethodGet, "/boards/"+boardID+"/lists", map[string]string{
		"fields": "id,name,closed,idBoard",
	}, &lists)

	return lists, err
}

func (c *TrelloClient) ListCards(boardID string, listID *string, includeClosed bool) ([]TrelloCard, error) {
	path := "/boards/" + boardID + "/cards"

	if listID != nil && *listID != "" {
		lists, err := c.ListLists(boardID)
		if err != nil {
			return nil, err
		}
		if !hasListID(lists, *listID) {
			return nil, fmt.Errorf("List does not belong to the allowed board")
		}
		path = "/lists/" + *listID + "/cards"
	}
	filter := "open"
	if includeClosed {
		filter = "all"
	}

	var cards []TrelloCard
	err := c.request(http.MethodGet, path, map[string]string{
		"fields": "id,name,desc,url,closed,idBoard,idList,due,dueComplete,labels,isTemplate,pos,dateLastActivity",
		"filter": filter,
	}, &cards)

	return cards, err
}

func (c *TrelloClient) GetCard(cardID string) (*TrelloCard, error) {
	var card TrelloCard
	err := c.request(http.MethodGet, "/cards/"+cardID, map[string]string{
		"fields": "id,name,desc,url,closed,idBoard,idList,due,dueComplete,labels,isTemplate,pos,dateLastActivity",
	}, &card)

	return &card, err
}

func (c *TrelloClient) ListAttachments(cardID string) ([]TrelloAttachment, error) {
	var attachments []TrelloAttachment
	err := c.request(http.MethodGet, "/cards/"+cardID+"/attachments", map[string]string{
		"fields": "id,name,url,mimeType,bytes,date,isUpload",
	}, &attachments)

	return attachments, err
}

func (c *TrelloClient) ListCardMembers(cardID string) ([]TrelloMember, error) {
	var members []TrelloMember
	err := c.request(http.MethodGet, "/cards/"+cardID+"/members", map[string]string{
		"fields": "id,username,fullName",
	}, &members)

	return members, err
}

func (c *TrelloClient) ListChecklists(cardID string) ([]TrelloChecklist, error) {
	var checklists []TrelloChecklist
	err := c.request(http.MethodGet, "/cards/"+cardID+"/checklists", map[string]string{
		"checkItems": "all",
		"fields":     "id,name,idCard",
	}, &checklists)

	return checklists, err
}

func (c *TrelloClient) GetChecklist(checklistID string) (*TrelloChecklist, error) {
	var checklist TrelloChecklist
	err := c.request(http.MethodGet, "/checklists/"+checklistID, map[string]string{
		"fields": "id,name,idCard",
	}, &checklist)

	return &checklist, err
}

func (c *TrelloClient) AddChecklist(cardID, name string) (*TrelloChecklist, error) {
	var checklist TrelloChecklist
	err := c.request(http.MethodPost, "/cards/"+cardID+"/checklists", map[string]string{
		"name": name,
	}, &checklist)

	return &checklist, err
}

func (c *TrelloClient) DeleteChecklist(checklistID string) error {
	return c.request(http.MethodDelete, "/checklists/"+checklistID, nil, nil)
}

func (c *TrelloClient) AddCheckItem(checklistID, name string, checked *bool) (*TrelloCheckItem, error) {
	query := map[string]string{"name": name}

	if checked != nil {
		query["checked"] = boolStr(*checked)
	}

	var item TrelloCheckItem
	err := c.request(http.MethodPost, "/checklists/"+checklistID+"/checkItems", query, &item)

	return &item, err
}

type UpdateCheckItemInput struct {
	Name    *string
	Checked *bool
	Due     *string
}

func (c *TrelloClient) UpdateCheckItem(checklistID, checkItemID string, input UpdateCheckItemInput) (*TrelloCheckItem, error) {
	checklist, err := c.GetChecklist(checklistID)
	if err != nil {
		return nil, err
	}
	if checklist.IDCard == "" {
		return nil, fmt.Errorf("Checklist has no parent card")
	}
	query := map[string]string{}

	if input.Name != nil {
		query["name"] = *input.Name
	}

	if input.Checked != nil {
		if *input.Checked {
			query["state"] = "complete"
		} else {
			query["state"] = "incomplete"
		}
	}

	if input.Due != nil {
		query["due"] = *input.Due
	}

	var item TrelloCheckItem
	err = c.request(http.MethodPut, "/cards/"+checklist.IDCard+"/checkItem/"+checkItemID, query, &item)

	return &item, err
}

func (c *TrelloClient) DeleteCheckItem(checklistID, checkItemID string) error {
	return c.request(http.MethodDelete, "/checklists/"+checklistID+"/checkItems/"+checkItemID, nil, nil)
}

func (c *TrelloClient) GetCardWithAttachments(cardID string) (*CardWithAttachments, error) {
	return c.GetCardSnapshot(cardID, true, true)
}

func (c *TrelloClient) GetCardSnapshot(cardID string, attachments, checklists bool) (*CardWithAttachments, error) {
	query := map[string]string{
		"fields":      "id,name,desc,url,closed,idBoard,idList,due,dueComplete,labels,isTemplate,pos,dateLastActivity",
		"attachments": boolStr(attachments),
	}
	if attachments {
		query["attachment_fields"] = "id,name,url,mimeType,bytes,date,isUpload"
	}
	if checklists {
		query["checklists"] = "all"
		query["checklist_fields"] = "id,name,idCard"
	} else {
		query["checklists"] = "none"
	}
	var out CardWithAttachments
	err := c.request(http.MethodGet, "/cards/"+cardID, query, &out)
	if !attachments {
		out.Omitted = append(out.Omitted, "attachments")
	}
	if !checklists {
		out.Omitted = append(out.Omitted, "checklists")
	}
	return &out, err
}

func (c *TrelloClient) CreateCard(input CreateCardInput) (*TrelloCard, error) {
	query := map[string]string{
		"idList": input.IDList,
		"name":   input.Name,
	}

	if input.Desc != nil {
		query["desc"] = *input.Desc
	}

	if input.Due != nil {
		query["due"] = *input.Due
	}

	if len(input.IDLabels) > 0 {
		query["idLabels"] = strings.Join(input.IDLabels, ",")
	}

	var card TrelloCard
	err := c.request(http.MethodPost, "/cards", query, &card)

	return &card, err
}

func (c *TrelloClient) UpdateCard(cardID string, input UpdateCardInput) (*TrelloCard, error) {
	query := map[string]string{}

	if input.Name != nil {
		query["name"] = *input.Name
	}

	if input.Desc != nil {
		query["desc"] = *input.Desc
	}

	if input.Due != nil {
		query["due"] = *input.Due
	}

	if input.DueComplete != nil {
		query["dueComplete"] = boolStr(*input.DueComplete)
	}

	if input.Closed != nil {
		query["closed"] = boolStr(*input.Closed)
	}

	if input.IDLabels != nil {
		query["idLabels"] = strings.Join(input.IDLabels, ",")
	}

	var card TrelloCard
	err := c.request(http.MethodPut, "/cards/"+cardID, query, &card)

	return &card, err
}

func (c *TrelloClient) MoveCard(cardID, idList string, position *string) (*TrelloCard, error) {
	var card TrelloCard
	query := map[string]string{"idList": idList}
	if position != nil {
		if *position != "top" && *position != "bottom" {
			return nil, fmt.Errorf("position must be top or bottom")
		}
		query["pos"] = *position
	}
	err := c.request(http.MethodPut, "/cards/"+cardID, query, &card)

	return &card, err
}

func (c *TrelloClient) AddComment(cardID, text string) (map[string]any, error) {
	var out map[string]any
	err := c.request(http.MethodPost, "/cards/"+cardID+"/actions/comments", map[string]string{
		"text": text,
	}, &out)

	return out, err
}

func (c *TrelloClient) ArchiveCard(cardID string) (*TrelloCard, error) {
	closed := true

	return c.UpdateCard(cardID, UpdateCardInput{Closed: &closed})
}

func (c *TrelloClient) AddAttachment(input AddAttachmentInput) (*TrelloAttachment, error) {
	if input.URL != nil && *input.URL != "" {
		query := map[string]string{"url": *input.URL}

		if input.Name != nil {
			query["name"] = *input.Name
		}

		if input.SetCover != nil {
			query["setCover"] = boolStr(*input.SetCover)
		}

		var attachment TrelloAttachment
		err := c.request(http.MethodPost, "/cards/"+input.CardID+"/attachments", query, &attachment)

		return &attachment, err
	}

	if input.FilePath != nil && *input.FilePath != "" {
		return c.uploadAttachmentFile(input)
	}

	return nil, &TrelloMcpError{Message: "Either url or file_path is required"}
}

func (c *TrelloClient) uploadAttachmentFile(input AddAttachmentInput) (*TrelloAttachment, error) {
	filePath := *input.FilePath

	fileName := filepath.Base(filePath)
	if input.Name != nil && *input.Name != "" {
		fileName = *input.Name
	}

	fileBuffer, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	mimeType := "application/octet-stream"
	if input.MimeType != nil && *input.MimeType != "" {
		mimeType = *input.MimeType
	}

	u, err := url.Parse(trelloAPIBase + "/cards/" + input.CardID + "/attachments")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("key", c.cfg.APIKey)
	q.Set("token", c.cfg.Token)

	if input.Name != nil && *input.Name != "" {
		q.Set("name", *input.Name)
	}

	if input.SetCover != nil {
		q.Set("setCover", boolStr(*input.SetCover))
	}

	u.RawQuery = q.Encode()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, fileName))
	header.Set("Content-Type", mimeType)

	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}

	if _, err := part.Write(fileBuffer); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, u.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, safeTransportError(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TrelloAPIError{Status: resp.StatusCode, Message: safeErrorMessage(resp)}
	}

	var attachment TrelloAttachment
	if err := json.NewDecoder(resp.Body).Decode(&attachment); err != nil {
		return nil, err
	}

	return &attachment, nil
}

func (c *TrelloClient) request(method, path string, query map[string]string, out any) error {
	u, err := url.Parse(trelloAPIBase + path)
	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("key", c.cfg.APIKey)
	q.Set("token", c.cfg.Token)

	for k, v := range query {
		q.Set(k, v)
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequest(method, u.String(), nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return safeTransportError(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &TrelloAPIError{Status: resp.StatusCode, Message: safeErrorMessage(resp)}
	}

	if out == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

// net/http URL errors include query credentials. Report only the underlying
// transport cause so failed requests never put a token into MCP output.
func safeTransportError(err error) error {
	if e, ok := err.(*url.Error); ok {
		return fmt.Errorf("Trello transport error: %v", e.Err)
	}
	return fmt.Errorf("Trello transport error")
}

func safeErrorMessage(resp *http.Response) string {
	raw, err := io.ReadAll(resp.Body)
	if err != nil || len(raw) == 0 {
		return resp.Status
	}

	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}

	if err := json.Unmarshal(raw, &payload); err == nil {
		if payload.Message != "" {
			return payload.Message
		}

		if payload.Error != "" {
			return payload.Error
		}
	}

	text := strings.TrimSpace(string(raw))
	if text != "" {
		return text
	}

	return resp.Status
}

func boolStr(v bool) string {
	if v {
		return "true"
	}

	return "false"
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}

	return false
}
