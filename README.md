# sjis-mcp

Shift JIS ファイルを扱うための Claude Code 用 MCP サーバーです。

古い Windows プロジェクト（Borland C++Builder 等）のように Shift JIS でソースコードが書かれている場合、
Claude Code の組み込み Read/Edit/Write ツールでは文字化けが発生します。
このサーバーを使うことで、ファイルを Shift JIS のまま保持しながら Claude が正確に読み書きできます。

## ツール一覧

| ツール | 説明 |
|--------|------|
| `read_sjis` | Shift JIS ファイルを読み込み、UTF-8 として返す（行範囲指定・検索対応） |
| `write_sjis` | UTF-8 文字列を Shift JIS でファイルに書き込む |
| `edit_sjis` | Shift JIS ファイル内の文字列を置換する（ファイルは SJIS のまま、Unicode NFC 正規化対応） |

## ビルド

```bash
GOPATH=/usr/share/gocode go build -o sjis-mcp .
```

Windows 向けクロスコンパイル:

```bash
GOPATH=/usr/share/gocode GOOS=windows GOARCH=amd64 go build -o sjis-mcp.exe .
```

## Claude Code への登録

### グローバル登録（全プロジェクト共通）

`~/.claude/claude_desktop_config.json` または Claude Code の MCP 設定:

```json
{
  "mcpServers": {
    "sjis": {
      "command": "/path/to/sjis-mcp"
    }
  }
}
```

Windows の場合:

```json
{
  "mcpServers": {
    "sjis": {
      "command": "C:\\tools\\sjis-mcp\\sjis-mcp.exe"
    }
  }
}
```

### プロジェクトローカル登録

プロジェクトルートに `.mcp.json` を置く:

```json
{
  "mcpServers": {
    "sjis": {
      "command": "C:\\tools\\sjis-mcp\\sjis-mcp.exe"
    }
  }
}
```

## CLAUDE.md への記載例

Shift JIS プロジェクトの `CLAUDE.md` に以下を追記することで、Claude が自動的に正しいツールを使うようになります:

```markdown
## 文字コードについて

このプロジェクトのソースファイル（.cpp, .h, .pas 等）はすべて **Shift JIS** です。

### ファイル操作のルール

- ファイルを読む場合: **必ず `read_sjis` ツールを使う**（組み込みの Read は使用禁止）
- ファイルを編集する場合: **必ず `edit_sjis` ツールを使う**（組み込みの Edit は使用禁止）
- 新規ファイルを作成する場合: **必ず `write_sjis` ツールを使う**（組み込みの Write は使用禁止）
- grep/find/diff 等のシェルコマンドは Bash ツールで直接実行して構わない
  - ただし grep で日本語を検索する場合は `LANG=ja_JP.SJIS grep ...` のように指定する
```

## v1.2.0 での改善点

- **`read_sjis`: 行範囲指定** — `line_start` / `line_end` パラメータで部分読み込みが可能に。大きなファイルでもトークン上限を超えずに読める
- **`read_sjis`: 検索機能** — `search` / `context_lines` パラメータで grep 相当の検索が MCP 内で完結。Shift JIS ファイル内の日本語検索も環境依存なし
- **`edit_sjis`: 日本語マッチング改善** — Unicode NFC 正規化により、日本語を含む `old_str` でのマッチ失敗を解消
- **`edit_sjis`: 診断情報改善** — マッチ失敗時に `old_str` の先頭3行それぞれについて類似行を表示

## 注意事項

- `edit_sjis` は `old_str` がファイル内に複数存在する場合はエラーになります。
  コンテキストを含めた一意な文字列を指定してください（Claude Code 標準の Edit と同じ挙動）。
- バイナリファイルには使用しないでください。
