package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
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
		Description: "Shift JIS エンコードのファイルを読み込み、UTF-8 文字列として返します。行番号付きで返すことも可能です。line_start/line_end で部分読み込みもできます。",
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
				"line_start": {
					Type:        "string",
					Description: "読み込み開始行番号（1始まり）。省略時はファイル先頭から",
				},
				"line_end": {
					Type:        "string",
					Description: "読み込み終了行番号（1始まり、この行も含む）。省略時はファイル末尾まで",
				},
				"search": {
					Type:        "string",
					Description: "検索文字列（UTF-8）。指定するとマッチした行とその前後 context_lines 行のみ返す（grep 相当）",
				},
				"context_lines": {
					Type:        "string",
					Description: "search 使用時、マッチ行の前後に表示する行数（デフォルト: 3）",
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

// ─── Unicode 正規化 ──────────────────────────────────────────────────────────

// normalizeUnicode は文字列を NFC 正規化する。
// Shift JIS → UTF-8 変換結果と Claude が送信する UTF-8 文字列で
// NFC/NFD の差異が生じうるため、比較前に統一する。
func normalizeUnicode(s string) string {
	return norm.NFC.String(s)
}

// ─── 診断: 最も近い候補行を探す ──────────────────────────────────────────────

// old_str の各行についてファイル中の類似行を探して返す
func findNearestMatch(content, oldStr string) string {
	normalizedOld := strings.ReplaceAll(oldStr, "\r\n", "\n")
	oldLines := strings.Split(normalizedOld, "\n")

	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalizedContent, "\n")

	// old_str の先頭から最大3行について候補を探す
	var sections []string
	for oi := 0; oi < len(oldLines) && oi < 3; oi++ {
		target := strings.TrimSpace(oldLines[oi])
		if target == "" {
			continue
		}
		targetNFC := normalizeUnicode(target)
		targetLower := strings.ToLower(targetNFC)

		var candidates []string
		for i, line := range lines {
			lineNFC := normalizeUnicode(strings.TrimSpace(line))
			lineLower := strings.ToLower(lineNFC)
			if lineLower == "" {
				continue
			}
			// ファイルの行が検索対象を含む、または検索対象がファイルの行を含む
			// （ただし短すぎる部分一致を除外するため、逆方向は行の長さが検索対象の30%以上の場合のみ）
			if strings.Contains(lineLower, targetLower) ||
				(len(lineLower) > 3 && len(lineLower)*100/len(targetLower) >= 30 && strings.Contains(targetLower, lineLower)) {
				candidates = append(candidates, fmt.Sprintf("    行 %d: %s", i+1, line))
				if len(candidates) >= 3 {
					break
				}
			}
		}
		if len(candidates) > 0 {
			sections = append(sections, fmt.Sprintf("  old_str の %d 行目 %q に近い候補:\n%s", oi+1, target, strings.Join(candidates, "\n")))
		} else {
			sections = append(sections, fmt.Sprintf("  old_str の %d 行目 %q → 類似行なし", oi+1, target))
		}
	}

	if len(sections) == 0 {
		return "（old_str に類似する行が見つかりませんでした）"
	}
	return strings.Join(sections, "\n")
}

// ─── 検索: grep 相当 ─────────────────────────────────────────────────────────

func searchInContent(content, query string, contextLines int) string {
	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalizedContent, "\n")

	// マッチ行を収集（NFC 正規化 + 大文字小文字無視）
	matchedLines := map[int]bool{}
	queryLower := strings.ToLower(normalizeUnicode(query))
	for i, line := range lines {
		if strings.Contains(strings.ToLower(normalizeUnicode(line)), queryLower) {
			matchedLines[i] = true
		}
	}
	if len(matchedLines) == 0 {
		return ""
	}

	// 表示行を決定（マッチ行 ± contextLines）
	showLines := map[int]bool{}
	for lineIdx := range matchedLines {
		for j := lineIdx - contextLines; j <= lineIdx+contextLines; j++ {
			if j >= 0 && j < len(lines) {
				showLines[j] = true
			}
		}
	}

	// 出力（非連続部分は "..." で区切る）
	sb := strings.Builder{}
	fmt.Fprintf(&sb, "[%d 件マッチ]\n", len(matchedLines))
	prevLine := -2
	for i := 0; i < len(lines); i++ {
		if !showLines[i] {
			continue
		}
		if i > prevLine+1 && prevLine >= 0 {
			sb.WriteString("  ...\n")
		}
		marker := " "
		if matchedLines[i] {
			marker = ">"
		}
		fmt.Fprintf(&sb, "%s%4d\t%s\n", marker, i+1, lines[i])
		prevLine = i
	}
	return sb.String()
}

// ─── edit_sjis 実装 ───────────────────────────────────────────────────────────

func editSJISByString(path, oldStr, newStr string, normalizeNL, replaceAll, dryRun bool) (string, error) {
	content, err := readSJIS(path)
	if err != nil {
		return "", err
	}

	// ── Step 1: マッチング用の正規化文字列を準備 ──
	// 改行正規化（CRLF → LF）
	hasCRLF := strings.Contains(content, "\r\n")
	workContent := content
	workOld := oldStr
	workNew := newStr
	if normalizeNL {
		workContent = strings.ReplaceAll(content, "\r\n", "\n")
		workOld = strings.ReplaceAll(oldStr, "\r\n", "\n")
		workNew = strings.ReplaceAll(newStr, "\r\n", "\n")
	}

	// Unicode NFC 正規化でマッチ位置を特定
	nfcContent := normalizeUnicode(workContent)
	nfcOld := normalizeUnicode(workOld)

	if !strings.Contains(nfcContent, nfcOld) {
		diag := findNearestMatch(content, oldStr)
		return "", fmt.Errorf("old_str が見つかりませんでした。\n%s\n\nヒント: normalize_newlines のデフォルトは true です。それでも一致しない場合は read_sjis で内容を確認し、行番号指定モード（line_start/line_end）の使用を検討してください。", diag)
	}

	count := strings.Count(nfcContent, nfcOld)

	if dryRun {
		var matchLines []int
		remaining := nfcContent
		offset := 0
		for {
			idx := strings.Index(remaining, nfcOld)
			if idx == -1 {
				break
			}
			lineNum := strings.Count(nfcContent[:offset+idx], "\n") + 1
			matchLines = append(matchLines, lineNum)
			offset += idx + len(nfcOld)
			remaining = nfcContent[offset:]
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

	// ── Step 2: 実際の置換 ──
	// NFC 正規化版でマッチした場合でも、元の workContent でもマッチするなら
	// 元の文字列を保持して置換する（不必要な NFC 変換を避ける）
	var newContent string
	if strings.Contains(workContent, workOld) {
		// 元のまま置換可能（NFC 正規化不要なケース）
		if replaceAll {
			newContent = strings.ReplaceAll(workContent, workOld, workNew)
		} else {
			newContent = strings.Replace(workContent, workOld, workNew, 1)
		}
	} else {
		// NFC 正規化しないとマッチしないケース: NFC 正規化版で置換
		nfcNew := normalizeUnicode(workNew)
		if replaceAll {
			newContent = strings.ReplaceAll(nfcContent, nfcOld, nfcNew)
		} else {
			newContent = strings.Replace(nfcContent, nfcOld, nfcNew, 1)
		}
	}

	if hasCRLF {
		newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
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

		// 検索モード
		searchStr, hasSearch := getString("search")
		if hasSearch {
			contextLines, _ := getInt("context_lines")
			if contextLines <= 0 {
				contextLines = 3
			}
			result := searchInContent(content, searchStr, contextLines)
			if result == "" {
				return errResult(fmt.Sprintf("検索文字列 %q が見つかりませんでした（%s）", searchStr, path))
			}
			return CallToolResult{
				Content: []ContentBlock{{Type: "text", Text: result}},
			}, nil
		}

		// 行分割
		allLines := strings.Split(content, "\n")
		totalLines := len(allLines)

		// 行範囲の決定
		lineStart := 1
		lineEnd := totalLines
		if ls, ok := getInt("line_start"); ok {
			if ls < 1 || ls > totalLines {
				return errResult(fmt.Sprintf("line_start=%d がファイルの行数(%d行)の範囲外です", ls, totalLines))
			}
			lineStart = ls
		}
		if le, ok := getInt("line_end"); ok {
			if le < lineStart || le > totalLines {
				return errResult(fmt.Sprintf("line_end=%d が無効です（line_start=%d, 総行数=%d）", le, lineStart, totalLines))
			}
			lineEnd = le
		}

		// 出力組み立て
		lineNumbers := getBool("line_numbers", false)
		sb := strings.Builder{}
		if lineStart != 1 || lineEnd != totalLines {
			fmt.Fprintf(&sb, "[%s: 行 %d〜%d / 全 %d 行]\n", path, lineStart, lineEnd, totalLines)
		}
		for i := lineStart - 1; i < lineEnd; i++ {
			if lineNumbers {
				fmt.Fprintf(&sb, "%4d\t%s\n", i+1, allLines[i])
			} else {
				sb.WriteString(allLines[i])
				sb.WriteByte('\n')
			}
		}
		return CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: sb.String()}},
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
				ServerInfo:      ServerInfo{Name: "sjis-mcp", Version: "1.2.0"},
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
