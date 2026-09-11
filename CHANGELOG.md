# CHANGELOG

Goulash 処理系の変更履歴です。v0.1 は未リリースのため、すべて Unreleased 扱いです。

## [Unreleased]

### 追加
- `ime()` / `imeget()`（GUI）：IME 変換入力と変換中文字列の取得
- `dim(n1, n2, ...)`：多次元配列の確保（`0` 埋め）
- map のドット記法（`m.name` ≡ `m["name"]`、読み書き・複合代入可）
- デフォルト引数（`def f(a, b=10)`、呼出時評価）
- 素の `enum` 文（連番整数定数、`Name_` 接頭辞、複数行・明示値・末尾カンマ可）
- `lextokens(src)` / `strwidth(s)`（エディタ支援：字句列と表示幅）
- `font(spec[, size])`（GUI）：フォントファイルパス・システムフォント名による書体指定（`GOULASH_FONT` 環境変数も対応）
- `button` の画像指定を4状態に拡張（通常・ホバー・押下・無効）
- `getkey` の検出キーを拡張（F1～F12・Shift/Ctrl/Alt・編集キー・記号キー、仮想キー番号準拠）
- GUI 入力系の拡充：ゲームパッド、タッチ、マウスホイール・カーソル、全画面、ウィンドウ移動・終了検出、ドロップファイル
- CUI/共通：`clipboard_get` / `clipboard_set`、`dlgopen` / `dlgsave`、`httpget`、`nanotime`、`getenv` / `setenv`、`open`、メモ帳・バイト列・配列操作群
- VS Code 拡張の雛形（`editors/vscode/`：`.gsh` の強調表示・編集支援）
- `setstr(id, s)`（GUI）：`mesbox` 内容の置換（`mesbox` は表示専用のため）
- `examples/human_check.gsh`：人手検証テスト（描画・入力・IME・音声・キーの確認手順）
- `pipeexec(name, args...)`：外部コマンドを実行し標準出力を文字列で返す（`exec` の取込版。起動失敗・非0終了はエラー）
- `asc(s)` / `chr(code)`：先頭文字のコードポイント取得とコードポイントの文字化（GUI `input()` の制御文字判定用）
- `inputbox` 変換中の caret 追従（GUI）：変換中文字列の分だけ caret・候補窓基準が進む（`inputbox` 前面時）
- `resizable([v])`（GUI）：ウィンドウのドラッグによるリサイズ許可・状態取得（描画解像度は `screen()` のまま）
- `inputbox` の自前実装（GUI）：カスタム描画の1行エディタ。Backspace・Delete・矢印・Home・End（長押しリピート付き）、IME変換対応（フォーカスで自動有効化、候補窓は caret 位置）
- GUI `input()` の即時モード：前回呼出以降の確定文字列を返却（IME確定分を含む。Backspace・Tab・Enter・Esc・Delete はコード `8`・`9`・`13`・`27`・`127` の制御文字として同じリピート付き）。行編集は `"\r"` での区切り・`asc()` との組合せで組む。`inputbox` フォーカス中は `""`
- `color(r, g, b [, a])`：α値（`0` 透明～`255` 不透明）に対応。GUI の文字・図形描画に適用（`galpha` とは乗算）。CUI の端末文字αは無視
- VS Code 拡張に F5 実行を追加（`F5` = `gsh run`、`Ctrl+F5` = `--gui` 付き。`gsh` の PATH 登録が前提）
- `#mode cli` / `#mode gui` 指示子：`gsh run` の実行モードをコード内で指定（`#include` 先も含めた有効行の最後が勝つ。REPL は常に CUI）
- `limit(v [, lo [, hi]])`：`lo` / `hi` の省略に対応（省略側は型の最小値 / 最大値。`limit(v)` は恒等）
- 引数の途中省略 `,,`：`null` と等価（`limit(v, , hi)` のように省略位置に既定値を持つ関数用。末尾カンマは無視）
- `httppost(url, body [, contentType [, path]])`：`httpget` の逆方向。`contentType` 既定 `application/json`、2xx以外・16MB超・10秒超過はエラー
- `mes()` / `print()` の末尾装飾指定：`bold` / `italic` / `bolditalic` / `underline`（完全一致のみ消費。GUIはフォント変形描画、CUIはANSI）
- ファイル組込の相対パス解決：スクリプト脇→カレントの順（`#include` と同じ規則。新規保存はスクリプト脇。`exec` / `pipeexec` のコマンド名は対象外）

### 変更（破壊的）
- コマンド名：`hsp-next` → `gou` → `gsh`。モジュールも `gsh`（`gsh/gui`）に統一。言語名 `Goulash` は維持
- スクリプト拡張子：`.hspn` → `.gou` → `.gsh` に統一
- `struct` を廃止し `map` に統一（宣言・リテラル・型・専用エラーごと削除）
- 配列の自動拡張を廃止。未確保・範囲外への代入はエラー。拡張は `push()` のみ
- map・配列の欠番キー読取をエラー化（`has()` で事前確認）
- `pos`：GUI ではピクセル基準に変更。CUI/GUI とも負値を許可
- `boxf()`：引数なしで全画面塗りつぶし
- `font`：同梱フォント（M+）を廃止し OS システムフォントを使用
- `goto` / `gosub` / `elif` の専用エラーを廃止。通常の識別子として使用可能
- `#enum` ディレクティブを廃止（不明なディレクティブ扱い）。連番定数は素の `enum` 文に一本化
- MANUAL から HSP 互換記述を削除
- `gzoom()` / `grotate()` を `gcopy()` に統合：`gcopy(src, sx, sy, w, h [, zx, zy [, deg]])`（5引数=等倍、7引数=拡縮、8引数=中心回転＋拡縮）
- `width()` を廃止し `screen()` に一本化
- `stick()` を廃止（方向・決定系は `getkey()`、クリックは `clicked()` で取得）
- `dim()` の初期値を `null` から `0` に変更
- `color()` の引数を 0/3 個から 0/3/4 個に拡張（`SetColor` 内部 I/F も RGBA 化）
- `Foreground()` / 図形描画系を `[3]int` から `[4]int`（RGBA）に変更
- `exec()` を非同期起動に変更（起動成功で `0` を返してすぐ戻る。終了コードは返さない。`pipeexec()` は終了待ち＋取込のまま）
- GUI のブロッキング行入力を廃止（`input()` は即時モードに一本化。CUI の `input([prompt])` は行単位のまま）
- `gsh run` の既定を GUI モードに変更（`#mode` 省略時はウィンドウを開く。`--gui` は強制指定として残し、`--cui` を追加。フラグは `#mode` より優先）
- GUI実行時エラーでプロセスを終了するように変更（従来は検査用にウィンドウを開いたままにしていた。メッセージは標準エラー出力へ出し、終了コード `1`。正常終了時は開いたまま）
- `mes()` / `print()` の末尾が装飾キーワードと完全一致する場合は印刷されず装飾指定になります（例：`mes("a", "bold")` は `a` を太字表示）
- `getpath()` のビットマスク数値を廃止し、部位キーワードの複数引数に変更（`getpath(p, "dir", "base")` 等。数値指定はエラー）
- ビルドタグ `gui` を廃止し単一バイナリ化（`go build` のみで CUI / GUI 両対応。F5 実行も特別なビルド不要）

### 修正
- ボタン画像がホバー・押下で切り替わらない不具合を修正（ebitenui の切替は `TextAndImage` 併用時のみ有効）
- ボタン画像の表示域ずれを修正（画像をボタンサイズに拡縮したスナップショットで表示）
- IME 変換中文字列の表示位置ずれを修正（セル換算ではなく実測ピクセル配置。候補窓の基準位置も同様。`inputbox` フォーカス時はボックス caret 基準になり、`(0,0)` への張り付きを解消）
- IME 確定後の Backspace ずれを修正（フィールド内削除を差分ミラーし、確定済み文字の残留・後続入力の呑み込みを解消。変換中の Backspace/Enter/Escape は IME 所有になり二重処理しない）
- VS Code の F5 を実コマンド化（`extension.js`：保存→ターミナル確保→`gsh run`。ターミナルなしの無反応を解消）
- `dialog` のはみ出しを修正（文を自動折り返し、窓サイズを内容に合わせる）
- `mesbox` を表示専用として明確化（キーボード編集不可。`setstr` / `getstr` で操作）
- ラベルなし画像ボタンの表示ずれを修正（空テキスト行による数ピクセルのオフセットを解消。画像単独時は状態フェイスで全面表示）
- エンジン（Ebiten）の panic がスタックダンプで表示される問題を修正（ゲームスレッド・スクリプトスレッドの両方を recover し、1行の `エラー:` 表示＋終了コード1に変換）
- `screen()` / `picload()` の各辺を 4096 までに制限（超過はスクリプトエラー。巨大画像のエンジン panic を防止）。`title()` の NUL 文字をエラー化

### 削除
- `keychar()`（導入直後に廃止。GUI `input()` の即時モードに一本化。確定文字列の受取は `input()`、変換中は `imeget()`）
- `toggle()`（導入直後に廃止。チェックボックスで代替）
- `stick()`（完全削除。`getkey()` / `clicked()` で代替）
- `width()`（完全削除。`screen()` に一本化）
- `gzoom()` / `grotate()`（完全削除。`gcopy()` の拡縮・回転引数に統合）
- `keyrep()`（完全削除。文字入力とリピートは GUI `input()` に一本化。物理キーは `getkey()` を使用）
- `redraw`（完全削除。使用時は未定義エラー）
- `struct` 関連の構文・値・組み込み・テスト・文書
- 同梱フォント `gui/mplus-1p-regular.ttf` と `gui/font-license.md`
- `palette` はトゥルーカラーのため非対応を明記
