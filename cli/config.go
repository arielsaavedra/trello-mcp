package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TrelloMcpError mirrors the TS base error class.
type TrelloMcpError struct {
	Message string
}

func (e *TrelloMcpError) Error() string {
	return e.Message
}

type AppConfig struct {
	APIKey             string
	Token              string
	AllowedBoardIDs    []string
	OnboardingRequired bool
	ConfigPath         string
	BoardIDsLocked     bool
	APIScope           string
}

var credentialEnvKeys = []string{"TRELLO_API_KEY", "TRELLO_TOKEN", "TRELLO_ALLOWED_BOARD_IDS"}

func loadConfig() (*AppConfig, error) {
	env := mergedEnv()
	apiScope := os.Getenv("TRELLO_API_SCOPE")
	if apiScope == "" {
		apiScope = "boards"
	}
	if apiScope != "boards" && apiScope != "account" {
		return nil, fmt.Errorf("TRELLO_API_SCOPE must be boards or account")
	}

	apiKey := strings.TrimSpace(env["TRELLO_API_KEY"])
	token := strings.TrimSpace(env["TRELLO_TOKEN"])
	configPath := resolveConfigPath(env)

	envBoardIDs := parseBoardIDs(env["TRELLO_ALLOWED_BOARD_IDS"])

	allowedBoardIDs := envBoardIDs
	if len(allowedBoardIDs) == 0 {
		fileBoardIDs, err := loadPersistedBoardIDs(configPath)
		if err != nil {
			return nil, err
		}

		allowedBoardIDs = fileBoardIDs
	}

	if apiKey == "" || token == "" {
		return nil, &TrelloMcpError{Message: missingCredentialMessage()}
	}

	return &AppConfig{
		APIKey:             apiKey,
		Token:              token,
		AllowedBoardIDs:    allowedBoardIDs,
		OnboardingRequired: len(allowedBoardIDs) == 0,
		ConfigPath:         configPath,
		BoardIDsLocked:     len(envBoardIDs) > 0,
		APIScope:           apiScope,
	}, nil
}

// mergedEnv resolves credentials from the process env, then .env files, then
// MCP client configs, mirroring credentials.ts#loadMergedEnv.
func mergedEnv() map[string]string {
	merged := map[string]string{}

	for _, key := range credentialEnvKeys {
		merged[key] = os.Getenv(key)
	}

	merged["TRELLO_CONFIG_PATH"] = os.Getenv("TRELLO_CONFIG_PATH")

	if !hasCredentials(merged) {
		fromDotenv := loadDotenvFiles(dotenvPaths())

		for _, key := range credentialEnvKeys {
			if strings.TrimSpace(merged[key]) == "" && strings.TrimSpace(fromDotenv[key]) != "" {
				merged[key] = strings.TrimSpace(fromDotenv[key])
			}
		}
	}

	if !hasCredentials(merged) {
		apiKey, token := loadMcpCredentials(mcpConfigPaths())

		if strings.TrimSpace(merged["TRELLO_API_KEY"]) == "" && apiKey != "" {
			merged["TRELLO_API_KEY"] = apiKey
		}

		if strings.TrimSpace(merged["TRELLO_TOKEN"]) == "" && token != "" {
			merged["TRELLO_TOKEN"] = token
		}
	}

	return merged
}

func hasCredentials(env map[string]string) bool {
	return strings.TrimSpace(env["TRELLO_API_KEY"]) != "" && strings.TrimSpace(env["TRELLO_TOKEN"]) != ""
}

func homeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return dir
}

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}

	return dir
}

func resolveConfigPath(env map[string]string) string {
	if custom := strings.TrimSpace(env["TRELLO_CONFIG_PATH"]); custom != "" {
		return custom
	}

	return filepath.Join(homeDir(), ".config", "trello-mcp", "config.json")
}

func dotenvPaths() []string {
	return []string{
		filepath.Join(cwd(), ".env"),
		filepath.Join(homeDir(), ".config", "trello-mcp", ".env"),
	}
}

func mcpConfigPaths() []string {
	return []string{
		filepath.Join(homeDir(), ".cursor", "mcp.json"),
		filepath.Join(cwd(), ".cursor", "mcp.json"),
	}
}

func loadDotenvFiles(paths []string) map[string]string {
	values := map[string]string{}

	for _, path := range paths {
		for k, v := range parseDotenv(readFileOrEmpty(path)) {
			values[k] = v
		}
	}

	return values
}

func parseDotenv(raw string) map[string]string {
	values := map[string]string{}

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "export ") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		}

		sep := strings.Index(trimmed, "=")
		if sep == -1 {
			continue
		}

		key := strings.TrimSpace(trimmed[:sep])
		value := strings.TrimSpace(trimmed[sep+1:])

		if len(value) >= 2 {
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
				value = value[1 : len(value)-1]
			}
		}

		values[key] = value
	}

	return values
}

type mcpConfigFile struct {
	McpServers map[string]struct {
		Env map[string]string `json:"env"`
	} `json:"mcpServers"`
}

func loadMcpCredentials(paths []string) (string, string) {
	for _, path := range paths {
		raw := readFileOrEmpty(path)
		if raw == "" {
			continue
		}

		var config mcpConfigFile
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			continue
		}

		server, ok := config.McpServers["trello"]
		if !ok {
			for _, candidate := range config.McpServers {
				if strings.TrimSpace(candidate.Env["TRELLO_API_KEY"]) != "" &&
					strings.TrimSpace(candidate.Env["TRELLO_TOKEN"]) != "" {
					server = candidate
					ok = true

					break
				}
			}
		}

		if !ok {
			continue
		}

		apiKey := strings.TrimSpace(server.Env["TRELLO_API_KEY"])
		token := strings.TrimSpace(server.Env["TRELLO_TOKEN"])

		if apiKey != "" || token != "" {
			return apiKey, token
		}
	}

	return "", ""
}

type boardStoreConfig struct {
	AllowedBoardIDs []string `json:"allowedBoardIds"`
	UpdatedAt       string   `json:"updatedAt,omitempty"`
}

func loadPersistedBoardIDs(configPath string) ([]string, error) {
	raw := readFileOrEmpty(configPath)
	if raw == "" {
		return []string{}, nil
	}

	var data boardStoreConfig
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}

	return uniqueNonEmpty(data.AllowedBoardIDs), nil
}

func saveAllowedBoardIDs(configPath string, boardIDs []string) error {
	unique := uniqueNonEmpty(boardIDs)

	if len(unique) == 0 {
		return &TrelloMcpError{Message: "At least one board id is required"}
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}

	payload := boardStoreConfig{
		AllowedBoardIDs: unique,
		UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, append(data, '\n'), 0o644)
}

func parseBoardIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}

	return uniqueNonEmpty(strings.Split(raw, ","))
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := []string{}

	for _, value := range values {
		trimmed := strings.TrimSpace(value)

		if trimmed == "" || seen[trimmed] {
			continue
		}

		seen[trimmed] = true

		result = append(result, trimmed)
	}

	return result
}

func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return string(data)
}

func missingCredentialMessage() string {
	return strings.Join([]string{
		"Trello credentials are required.",
		"Provide them via one of:",
		"  1. Environment: TRELLO_API_KEY and TRELLO_TOKEN",
		"  2. ~/.cursor/mcp.json (mcpServers.trello.env)",
		"  3. .env or ~/.config/trello-mcp/.env",
		"  4. Interactive: trello-mcp onboard (prompts when missing)",
	}, "\n")
}

func onboardingRequiredMessage() string {
	return strings.Join([]string{
		"Trello board onboarding is required.",
		"1. Call list_available_boards",
		"2. Ask the user which board(s) to use",
		"3. Call select_allowed_boards with the chosen board ids",
	}, "\n")
}

func assertBoardAllowed(boardID string, cfg *AppConfig) error {
	if cfg.OnboardingRequired {
		return &TrelloMcpError{Message: "Board setup is incomplete. Use list_available_boards and select_allowed_boards first."}
	}

	if !contains(cfg.AllowedBoardIDs, boardID) {
		return &TrelloMcpError{Message: fmt.Sprintf("Board %s is not in TRELLO_ALLOWED_BOARD_IDS", boardID)}
	}

	return nil
}
