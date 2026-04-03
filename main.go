package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// ─── JSON-RPC / MCP 基本型 ───────────────────────────────────────────────────

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─── MCP プロトコル型 ─────────────────────────────────────────────────────────

type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct{}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ─── ツール定義 ───────────────────────────────────────────────────────────────

var tools = []Tool{
	{
		Name:        "read_sjis",
		Description: "Shift JIS エンコードのファイルを読み込み、UTF-8 文字列として返します。",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "読み込むファイルのパス",
				},
			},
			Required: []string{"path"},
		},
	},
	{
		Name:        "write_sjis",
		Description: "UTF-8 文字列を Shift JIS エンコードでファイルに書き込みます。",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "書き込み先ファイルのパス",
				},
				"content": {
					Type:        "string",
					Description: "書き込む内容（UTF-8 文字列）",
				},
			},
			Required: []string{"path", "content"},
		},
	},
	{
		Name:        "edit_sjis",
		Description: "Shift JIS ファイル内の特定の文字列を別の文字列に置換します。元のファイルは Shift JIS のまま保持されます。",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "編集するファイルのパス",
				},
				"old_str": {
					Type:        "string",
					Description: "置換対象の文字列（UTF-8 で指定）",
				},
				"new_str": {
					Type:        "string",
					Description: "置換後の文字列（UTF-8 で指定）",
				},
			},
			Required: []string{"path", "old_str", "new_str"},
		},
	},
}

// ─── 文字コード変換 ───────────────────────────────────────────────────────────

func readSJIS(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("ファイルの読み込みに失敗しました: %w", err)
	}
	decoder := japanese.ShiftJIS.NewDecoder()
	utf8, _, err := transform.Bytes(decoder, data)
	if err != nil {
		return "", fmt.Errorf("Shift JIS → UTF-8 変換に失敗しました: %w", err)
	}
	return string(utf8), nil
}

func writeSJIS(path, content string) error {
	encoder := japanese.ShiftJIS.NewEncoder()
	sjis, _, err := transform.Bytes(encoder, []byte(content))
	if err != nil {
		return fmt.Errorf("UTF-8 → Shift JIS 変換に失敗しました: %w", err)
	}
	if err := os.WriteFile(path, sjis, 0644); err != nil {
		return fmt.Errorf("ファイルの書き込みに失敗しました: %w", err)
	}
	return nil
}

func editSJIS(path, oldStr, newStr string) (string, error) {
	content, err := readSJIS(path)
	if err != nil {
		return "", err
	}
	if !strings.Contains(content, oldStr) {
		return "", fmt.Errorf("old_str が見つかりませんでした:\n%s", oldStr)
	}
	count := strings.Count(content, oldStr)
	if count > 1 {
		return "", fmt.Errorf("old_str がファイル内に %d 箇所見つかりました。一意に特定できる文字列を指定してください", count)
	}
	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := writeSJIS(path, newContent); err != nil {
		return "", err
	}
	return fmt.Sprintf("編集が完了しました（%s）", path), nil
}

// ─── ツール呼び出しハンドラ ───────────────────────────────────────────────────

func handleCallTool(params json.RawMessage) (interface{}, *RPCError) {
	var p CallToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	getString := func(key string) (string, bool) {
		v, ok := p.Arguments[key]
		if !ok {
			return "", false
		}
		s, ok := v.(string)
		return s, ok
	}

	errResult := func(msg string) (interface{}, *RPCError) {
		return CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: msg}},
			IsError: true,
		}, nil
	}

	switch p.Name {
	case "read_sjis":
		path, ok := getString("path")
		if !ok {
			return errResult("path が指定されていません")
		}
		content, err := readSJIS(path)
		if err != nil {
			return errResult(err.Error())
		}
		return CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: content}},
		}, nil

	case "write_sjis":
		path, ok := getString("path")
		if !ok {
			return errResult("path が指定されていません")
		}
		content, ok := getString("content")
		if !ok {
			return errResult("content が指定されていません")
		}
		if err := writeSJIS(path, content); err != nil {
			return errResult(err.Error())
		}
		return CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("書き込みが完了しました（%s）", path)}},
		}, nil

	case "edit_sjis":
		path, ok := getString("path")
		if !ok {
			return errResult("path が指定されていません")
		}
		oldStr, ok := getString("old_str")
		if !ok {
			return errResult("old_str が指定されていません")
		}
		newStr, _ := getString("new_str") // 空文字列（削除）も正常ケース
		msg, err := editSJIS(path, oldStr, newStr)
		if err != nil {
			return errResult(err.Error())
		}
		return CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: msg}},
		}, nil

	default:
		return errResult(fmt.Sprintf("不明なツール: %s", p.Name))
	}
}

// ─── メインループ ─────────────────────────────────────────────────────────────

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 最大 10MB
	encoder := json.NewEncoder(os.Stdout)

	send := func(resp Response) {
		encoder.Encode(resp)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			send(Response{
				JSONRPC: "2.0",
				Error:   &RPCError{Code: -32700, Message: "Parse error"},
			})
			continue
		}

		var result interface{}
		var rpcErr *RPCError

		switch req.Method {
		case "initialize":
			result = InitializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities:    Capabilities{Tools: &ToolsCapability{}},
				ServerInfo:      ServerInfo{Name: "sjis-mcp", Version: "1.0.0"},
			}

		case "notifications/initialized":
			// 通知なので応答不要
			continue

		case "tools/list":
			result = ListToolsResult{Tools: tools}

		case "tools/call":
			result, rpcErr = handleCallTool(req.Params)

		case "ping":
			result = map[string]interface{}{}

		default:
			rpcErr = &RPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)}
		}

		send(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
			Error:   rpcErr,
		})
	}
}
