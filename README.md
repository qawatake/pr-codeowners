# pr-codeowners

GitHub PRの変更ファイルに対するコードオーナーと、各チームに所属するレビュアーを表示するCLIツール。

## インストール

```bash
go install github.com/qawatake/pr-codeowners@latest
```

## 使い方

```bash
./pr-codeowners <PR番号 or URL>

# 詳細ログを表示
./pr-codeowners -v <PR番号 or URL>
```

## 出力例

```json
{
  "@org/frontend-team": {
    "files": ["src/components/Button.tsx", "src/App.tsx"],
    "reviewers": ["user1", "user2"]
  },
  "@org/backend-team": {
    "files": ["api/handler.go"]
  },
  "(no owner)": {
    "files": ["README.md"]
  }
}
```

## 必要条件

- Go 1.21+
- GitHub CLI (`gh`) がインストール・認証済みであること
