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
		Description: "Shift JIS エンコードのファイルを読み込み、UTF-8 文字列として返します。行番号付きで返すことも可能です。",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "読み込むファイルのパス",
				},
				"line_numbers": {
					Type:        "boolean",
					Description: "true にすると各行の先頭に行番号を付けて返す（edit_sjis で行番号指定する際に便利）",
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
		Name: "edit_sjis",
		Description: `Shift JIS ファイルを編集します。2つのモードがあります。

【文字列置換モード】old_str / new_str を指定
- old_str に一致する箇所を new_str に置換します
- normalize_newlines: true（デフォルト）で CRLF/LF の違いを無視してマッチします
- replace_all: true で全出現箇所を置換します（デフォルトは false で1箇所のみ）
- dry_run: true にするとファイルを変更せず、マッチ結果のみ返します
- マッチしない場合は診断情報（最も近い候補行）を返します

【行番号置換モード】line_start / line_end / new_str を指定
- 指定した行範囲を new_str の内容で置き換えます
- read_sjis で行番号を確認してから使うと確実です`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "編集するファイルのパス",
				},
				"old_str": {
					Type:        "string",
					Description: "【文字列置換モード】置換対象の文字列（UTF-8 で指定）",
				},
				"new_str": {
					Type:        "string",
					Description: "置換後の文字列（UTF-8 で指定）。行番号モードでも使用",
				},
				"normalize_newlines": {
					Type:        "boolean",
					Description: "true（デフォルト）で old_str の \\n を \\r\\n にも自動マッチ。CRLF ファイルに有効",
				},
				"replace_all": {
					Type:        "boolean",
					Description: "true で全出現箇所を置換（デフォルト false）",
				},
				"dry_run": {
					Type:        "boolean",
					Description: "true にするとファイルを変更せず、マッチ結果のみ返す",
				},
				"line_start": {
					Type:        "string",
					Description: "【行番号置換モード】置換開始行番号（1始まり）",
				},
				"line_end": {
					Type:        "string",
					Description: "【行番号置換モード】置換終了行番号（1始まり、この行も含む）",
				},
			},
			Required: []string{"path", "new_str"},
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

// ─── 診断: 最も近い候補行を探す ──────────────────────────────────────────────

// old_str の最初の行がファイル中の何行目に出現するかを探して返す
func findNearestMatch(content, oldStr string) string {
	// old_str の最初の行を取得（正規化後）
	normalizedOld := strings.ReplaceAll(oldStr, "\r\n", "\n")
	firstLine := strings.SplitN(normalizedOld, "\n", 2)[0]
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return ""
	}

	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalizedContent, "\n")

	var candidates []string
	for i, line := range lines {
		if strings.Contains(strings.TrimSpace(line), firstLine) ||
			strings.Contains(firstLine, strings.TrimSpace(line)) {
			candidates = append(candidates, fmt.Sprintf("  行 %d: %s", i+1, line))
			if len(candidates) >= 5 {
				break
			}
		}
	}
	if len(candidates) == 0 {
		return fmt.Sprintf("（old_str の最初の行 %q に類似する行も見つかりませんでした）", firstLine)
	}
	return "old_str の最初の行に近い候補:\n" + strings.Join(candidates, "\n")
}

// ─── edit_sjis 実装 ───────────────────────────────────────────────────────────

func editSJISByString(path, oldStr, newStr string, normalizeNL, replaceAll, dryRun bool) (string, error) {
	content, err := readSJIS(path)
	if err != nil {
		return "", err
	}

	// マッチング用のコンテンツと old_str を準備
	searchContent := content
	searchOld := oldStr
	if normalizeNL {
		// CRLF → LF に正規化してマッチング
		searchContent = strings.ReplaceAll(content, "\r\n", "\n")
		searchOld = strings.ReplaceAll(oldStr, "\r\n", "\n")
	}

	if !strings.Contains(searchContent, searchOld) {
		diag := findNearestMatch(content, oldStr)
		return "", fmt.Errorf("old_str が見つかりませんでした。\n%s\n\nヒント: normalize_newlines のデフォルトは true です。それでも一致しない場合は read_sjis で内容を確認し、行番号指定モード（line_start/line_end）の使用を検討してください。", diag)
	}

	count := strings.Count(searchContent, searchOld)

	if dryRun {
		// マッチした行番号を収集
		var matchLines []int
		remaining := searchContent
		offset := 0
		for {
			idx := strings.Index(remaining, searchOld)
			if idx == -1 {
				break
			}
			lineNum := strings.Count(searchContent[:offset+idx], "\n") + 1
			matchLines = append(matchLines, lineNum)
			offset += idx + len(searchOld)
			remaining = searchContent[offset:]
			if !replaceAll {
				break
			}
		}
		lineNums := make([]string, len(matchLines))
		for i, l := range matchLines {
			lineNums[i] = fmt.Sprintf("%d", l)
		}
		return fmt.Sprintf("[dry-run] %d 箇所マッチしました（行: %s）。実際の変更は行いません。", count, strings.Join(lineNums, ", ")), nil
	}

	// 実際の置換（元の content に対して行う。CRLF を保持するため）
	// normalize_newlines が true の場合、content の CRLF を LF に正規化してから置換
	var newContent string
	if normalizeNL {
		// content を正規化した上で置換し、その後 newStr の改行を CRLF に合わせる
		// 元ファイルが CRLF なら結果も CRLF に保つ
		hasCRLF := strings.Contains(content, "\r\n")
		normalized := strings.ReplaceAll(content, "\r\n", "\n")
		normalizedNewStr := strings.ReplaceAll(newStr, "\r\n", "\n")
		if replaceAll {
			newContent = strings.ReplaceAll(normalized, searchOld, normalizedNewStr)
		} else {
			newContent = strings.Replace(normalized, searchOld, normalizedNewStr, 1)
		}
		if hasCRLF {
			newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
		}
	} else {
		if replaceAll {
			newContent = strings.ReplaceAll(content, oldStr, newStr)
		} else {
			newContent = strings.Replace(content, oldStr, newStr, 1)
		}
	}

	if err := writeSJIS(path, newContent); err != nil {
		return "", err
	}

	if replaceAll {
		return fmt.Sprintf("編集完了（%s）: %d 箇所を置換しました。", path, count), nil
	}
	return fmt.Sprintf("編集完了（%s）", path), nil
}

func editSJISByLineRange(path string, lineStart, lineEnd int, newStr string, dryRun bool) (string, error) {
	content, err := readSJIS(path)
	if err != nil {
		return "", err
	}

	// 改行コードを記憶
	hasCRLF := strings.Contains(content, "\r\n")
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	total := len(lines)
	if lineStart < 1 || lineStart > total {
		return "", fmt.Errorf("line_start=%d がファイルの行数(%d行)の範囲外です", lineStart, total)
	}
	if lineEnd < lineStart || lineEnd > total {
		return "", fmt.Errorf("line_end=%d が無効です（line_start=%d, 総行数=%d）", lineEnd, lineStart, total)
	}

	if dryRun {
		replaced := lines[lineStart-1 : lineEnd]
		return fmt.Sprintf("[dry-run] 行 %d〜%d を置換予定:\n%s", lineStart, lineEnd, strings.Join(replaced, "\n")), nil
	}

	normalizedNewStr := strings.ReplaceAll(newStr, "\r\n", "\n")
	newLines := strings.Split(normalizedNewStr, "\n")

	result := make([]string, 0, len(lines)-int(lineEnd-lineStart)+len(newLines))
	result = append(result, lines[:lineStart-1]...)
	result = append(result, newLines...)
	result = append(result, lines[lineEnd:]...)

	newContent := strings.Join(result, "\n")
	if hasCRLF {
		newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
	}

	if err := writeSJIS(path, newContent); err != nil {
		return "", err
	}
	return fmt.Sprintf("編集完了（%s）: 行 %d〜%d を置換しました。", path, lineStart, lineEnd), nil
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
	getBool := func(key string, defaultVal bool) bool {
		v, ok := p.Arguments[key]
		if !ok {
			return defaultVal
		}
		b, ok := v.(bool)
		if !ok {
			return defaultVal
		}
		return b
	}
	getInt := func(key string) (int, bool) {
		v, ok := p.Arguments[key]
		if !ok {
			return 0, false
		}
		switch n := v.(type) {
		case float64:
			return int(n), true
		case string:
			var i int
			_, err := fmt.Sscanf(n, "%d", &i)
			return i, err == nil
		}
		return 0, false
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
		lineNumbers := getBool("line_numbers", false)
		if lineNumbers {
			lines := strings.Split(content, "\n")
			sb := strings.Builder{}
			for i, line := range lines {
				fmt.Fprintf(&sb, "%4d\t%s\n", i+1, line)
			}
			content = sb.String()
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
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("書き込み完了（%s）", path)}},
		}, nil

	case "edit_sjis":
		path, ok := getString("path")
		if !ok {
			return errResult("path が指定されていません")
		}
		newStr, ok := getString("new_str")
		if !ok {
			return errResult("new_str が指定されていません")
		}
		dryRun := getBool("dry_run", false)

		// 行番号モード
		lineStart, hasLineStart := getInt("line_start")
		lineEnd, hasLineEnd := getInt("line_end")
		if hasLineStart || hasLineEnd {
			if !hasLineStart || !hasLineEnd {
				return errResult("line_start と line_end は両方指定してください")
			}
			msg, err := editSJISByLineRange(path, lineStart, lineEnd, newStr, dryRun)
			if err != nil {
				return errResult(err.Error())
			}
			return CallToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
		}

		// 文字列置換モード
		oldStr, ok := getString("old_str")
		if !ok {
			return errResult("old_str または line_start/line_end を指定してください")
		}
		normalizeNL := getBool("normalize_newlines", true)
		replaceAll := getBool("replace_all", false)

		msg, err := editSJISByString(path, oldStr, newStr, normalizeNL, replaceAll, dryRun)
		if err != nil {
			return errResult(err.Error())
		}
		return CallToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil

	default:
		return errResult(fmt.Sprintf("不明なツール: %s", p.Name))
	}
}

// ─── メインループ ─────────────────────────────────────────────────────────────

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
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
			send(Response{JSONRPC: "2.0", Error: &RPCError{Code: -32700, Message: "Parse error"}})
			continue
		}

		var result interface{}
		var rpcErr *RPCError

		switch req.Method {
		case "initialize":
			result = InitializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities:    Capabilities{Tools: &ToolsCapability{}},
				ServerInfo:      ServerInfo{Name: "sjis-mcp", Version: "1.1.0"},
			}
		case "notifications/initialized":
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

		send(Response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr})
	}
}
