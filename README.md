# senes

ミニマルなtodoサーバー。Web画面とCLIからタスクをCRUDできます。

- status: `doing` / `todo` / `waiting` / `backlog` / `done`
- タスクは並び替え可能（Web画面ではドラッグ&ドロップ、CLIでは `senes mv`）
- タイトル・説明欄でmarkdownが使えます（GFM）
- GitHub PRへのリンクは、Web画面でPRの状態（open / draft / merged / closed）に応じたアイコンと色で表示されます

## 使い方

```sh
go build -o senes .
./senes serve                # http://127.0.0.1:7878
```

```sh
senes ls                     # done以外を一覧表示（-a で全件）
senes ls doing waiting       # statusで絞り込み
senes add "PRをレビュー" -d "https://github.com/owner/repo/pull/1" -s doing
senes show 1
senes edit 1 -t "新しいタイトル" -s waiting
echo "- [ ] step" | senes edit 1 -d -   # 説明欄をstdinから読む
senes mv 1 done
senes mv 3 -top              # 同じstatus内で先頭へ（-bottom で末尾）
senes mv 3 -before 5         # #5 の直前へ（-after で直後）。statusも同時に変えられる: senes mv 3 doing -after 5
senes rm 1
```

## AI agent 向けskill

[`skills/senes/SKILL.md`](skills/senes/SKILL.md) に、AI agent（Claude Code など）が senes CLI でタスクを参照・作成・更新するためのskillを同梱しています。ステータスの選び方などのルールが書かれているので、agentにsenesを使わせる場合は導入してください。

```sh
# Claude Code の例（ユーザースコープに導入）
mkdir -p ~/.claude/skills
ln -s "$(pwd)/skills/senes" ~/.claude/skills/senes
```

## 設定

| 環境変数 | 説明 | デフォルト |
| --- | --- | --- |
| `SENES_URL` | CLIの接続先 | `http://127.0.0.1:7878` |
| `SENES_DB` | データファイル | `~/.local/share/senes/tasks.json` |
| `GITHUB_TOKEN` / `GH_TOKEN` | PRステータス取得用。未設定なら `gh auth token` を使用 | — |

## 名前の由来

senes は seneschal（中世ヨーロッパの領主館で、家政や日々の雑務を取り仕切った執事・家令）に由来します。日々のタスクを預かって管理する役割になぞらえています。

## ライセンス

[GNU Affero General Public License v3.0](LICENSE)（AGPL-3.0-only）
