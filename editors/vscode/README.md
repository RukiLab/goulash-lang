# Goulash for VS Code

Goulash（`.gsh`）用の言語サポートです。TextMate による構文強調表示、
コメント・括弧の編集支援を提供します。

## 導入方法（ローカル）

1. このフォルダを拡張機能ディレクトリにコピー（またはシンボリックリンク）します。
   - Windows: `%USERPROFILE%\.vscode\extensions\goulash-0.1.0\`
   - macOS / Linux: `~/.vscode/extensions/goulash-0.1.0/`
2. VS Code を再読み込みし、`.gsh` ファイルを開きます。

## 公開手順（管理者向け）

```sh
npm install -g @vscode/vsce
vsce package
vsce publish
```

## 内容

- `syntaxes/goulash.tmLanguage.json` — キーワード・組み込み関数・`#` ディレクティブ
  （`#include` / `#define` / `#ifdef` / `#ifndef` / `#else` / `#endif` /
  `#error`）、文字列、コメント、`0x` / `0b` / `0o` 数値
- `language-configuration.json` — `//` と `/* */` コメント、括弧、
  自動補完ペア、ブレースのインデント
- `package.json` — `.gsh` に対する言語・文法の登録

組み込み関数の一覧は処理系の登録内容（`builtin_*.go`）を反映しています。
組み込み関数を変更した場合は、`register("name", ...)` の出現箇所から
`support.function` の選択肢を再生成してください。
