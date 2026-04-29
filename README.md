# sjis-mcp

Shift JIS ファイルを扱うための Claude Code 用 MCP サーバーです。

古い Windows プロジェクト（Borland C++Builder 等）のように Shift JIS でソースコードが書かれている場合、
Claude Code の組み込み Read/Edit/Write ツールでは文字化けが発生します。
このサーバーを使うことで、ファイルを Shift JIS のまま保持しながら Claude が正確に読み書きできます。

## ツール一覧

| ツール | 説明 |
|--------|------|
| `read_sjis` | Shift JIS ファイルを読み込み、UTF-8 として返す（行範囲指定・検索対応、edit ヒント付き） |
| `write_sjis` | UTF-8 文字列を Shift JIS で**新規ファイル**として書き込む（既存ファイルは拒否） |
| `edit_sjis` | Shift JIS ファイル内の文字列を置換する（dry_run で diff 表示、CRLF/LF 自動保持） |

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
- 既存ファイルを編集する場合: **必ず `edit_sjis` ツールを使う**（組み込みの Edit は使用禁止）
  - 一部置換は `old_str` / `new_str`、行範囲置換は `line_start` / `line_end` モードを使い分ける
  - 大きい変更は事前に `dry_run: true` で diff を確認すると安全
- 新規ファイルを作成する場合のみ `write_sjis` を使う（既存ファイルは拒否される）
- grep/find/diff 等のシェルコマンドは Bash ツールで直接実行して構わない
  - ただし grep で日本語を検索する場合は `LANG=ja_JP.SJIS grep ...` のように指定する
```

## v1.3.0 での改善点

実利用フィードバックに基づく改善：

- **`write_sjis` を新規作成専用に変更** — 既存ファイルがあれば拒否します。誤上書きを構造的に防止し、エラーメッセージで `edit_sjis` への切り替えを誘導します
- **`edit_sjis`: dry_run で diff 表示** — マッチ箇所と置換前/置換後の内容を `-` / `+` 付きで表示。実行前の確認が容易に
- **改行コード（CRLF/LF）を結果メッセージに明示** — 編集・書き込み完了時に保存された改行コードを表示。BCB 系の CRLF ソースが維持されているかを確認可能
- **`read_sjis` 出力に `edit_sjis` 行範囲モードへのヒント** — 行番号付き / 部分読み込み / 検索モードの結果末尾に、そのまま `edit_sjis` の `line_start` / `line_end` で呼び出すための例を表示

## v1.2.0 での改善点

- **`read_sjis`: 行範囲指定** — `line_start` / `line_end` パラメータで部分読み込みが可能に。大きなファイルでもトークン上限を超えずに読める
- **`read_sjis`: 検索機能** — `search` / `context_lines` パラメータで grep 相当の検索が MCP 内で完結。Shift JIS ファイル内の日本語検索も環境依存なし
- **`edit_sjis`: 日本語マッチング改善** — Unicode NFC 正規化により、日本語を含む `old_str` でのマッチ失敗を解消
- **`edit_sjis`: 診断情報改善** — マッチ失敗時に `old_str` の先頭3行それぞれについて類似行を表示

## 注意事項

- `edit_sjis` は `old_str` がファイル内に複数存在する場合、`replace_all: true` を指定しないとエラーになります。
  コンテキストを含めた一意な文字列を指定するか、`replace_all` を明示してください。
- `write_sjis` は新規ファイル作成専用です。既存ファイルの編集は `edit_sjis` を使用してください。
- バイナリファイルには使用しないでください。
