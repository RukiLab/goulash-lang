// Command-line interface for the Goulash language v0.1 interpreter.
//
// Usage:
//
//	gsh run <file.gsh> [--keep]
//	gsh repl
//	gsh lex <file.gsh>
//	gsh parse <file.gsh>
//
// --keep is accepted by `run` for forward compatibility (a future
// transpiler backend may emit intermediate files) but is currently a no-op:
// the tree-walk interpreter generates no intermediate files.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gsh/gui"
)

const usage = `gsh: Goulash v0.1 インタプリタ

使い方:
  gsh run <file.gsh> [--keep] [--gui] [--cui] [-- args...]   スクリプトを実行します
  gsh repl                               対話環境（REPL）を起動します
  gsh lex <file.gsh>                    字句トークン列を出力します（デバッグ用）
  gsh parse <file.gsh>                  構文木（AST）を出力します（デバッグ用）

実行モードはコード内の #mode cli/gui で指定します（省略時は gui で
ウィンドウを開きます。--gui/--cui はコマンドラインからの強制指定で、
#mode より優先されます）。
--keep は将来の互換性のために予約されており、現在は何も行いません。`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "repl":
		cmdRepl()
	case "lex":
		cmdLex(os.Args[2:])
	case "parse":
		cmdParse(os.Args[2:])
	case "help", "--help", "-h":
		fmt.Println(usage)
	default:
		fmt.Fprintf(os.Stderr, "不明なコマンド %q です\n\n%s\n", os.Args[1], usage)
		os.Exit(2)
	}
}

func cmdRun(args []string) {
	var file string
	var scriptArgs []string
	forceGUI := false
	forceCUI := false
	verbatim := false // after `--`: everything is a script argument
	for _, a := range args {
		if verbatim {
			scriptArgs = append(scriptArgs, a)
			continue
		}
		if a == "--" {
			verbatim = true
			continue
		}
		if a == "--keep" {
			continue // reserved; tree-walk generates no files
		}
		if a == "--gui" {
			forceGUI = true
			continue
		}
		if a == "--cui" {
			forceCUI = true
			continue
		}
		if file == "" && strings.HasPrefix(a, "-") {
			fmt.Fprintf(os.Stderr, "不明なフラグ %q です\n", a)
			os.Exit(2)
		}
		if file == "" {
			file = a
			continue
		}
		// Extra positionals after the file are script arguments, so
		// `run game.gsh --hard` just works (`--` still recommended
		// when an argument itself starts with `-`).
		scriptArgs = append(scriptArgs, a)
	}
	if file == "" {
		fmt.Fprintln(os.Stderr, "run にはスクリプトファイルが必要です")
		os.Exit(2)
	}
	prog, mode, err := ParseFileMode(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	// Precedence: --gui/--cui flags beat the in-code #mode; without
	// flags #mode cli runs console, otherwise (gui or omitted) a
	// window opens.
	guiMode := mode == "gui"
	if forceCUI {
		guiMode = false
	}
	if forceGUI {
		guiMode = true
	}
	if guiMode {
		runGUI(file, prog, scriptArgs)
		return
	}
	in := NewInterp(os.Stdout)
	in.SetArgs(scriptArgs)
	in.SetScriptDir(scriptDirOf(file))
	if err := in.Run(prog); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	if code, ok := in.ExitCode(); ok {
		os.Exit(code)
	}
}

// scriptDirOf returns the absolute directory containing file (""
// when it cannot be determined); file builtins resolve relative
// paths there.
func scriptDirOf(file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return ""
	}
	return filepath.Dir(abs)
}

// enginePanicMsg renders a recovered engine (Ebiten) panic as one clean
// line: the first line of its message, without the Go stack trace.
// A script must never see engine internals as a crash dump.
func enginePanicMsg(r any) string {
	s := strings.TrimSpace(fmt.Sprint(r))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if s == "" {
		s = "不明なエンジンエラー"
	}
	return s
}

// runGUI executes prog in a window.
func runGUI(file string, prog *Program, scriptArgs []string) {
	_ = file
	wb, err := gui.New(640, 480)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	in := NewInterpWithBackend(wb, os.Stdin)
	in.SetArgs(scriptArgs)
	in.SetScriptDir(scriptDirOf(file))
	// The script runs on its own goroutine (see RunLoop) while the game
	// loop owns this one, so the result travels over a channel. Buffered
	// so a script finishing after an early window close never blocks.
	// A closed window with a still-running script reads as no error.
	runCh := make(chan error, 1)
	// Ebiten panics on engine misuse (oversized images, disposed
	// resources...), whether surfaced from the game thread or a
	// builtin on the script thread. Both become a clean error and
	// exit 1, never a Go stack trace.
	loopCode := func() (code int) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintln(os.Stderr, "エラー:", enginePanicMsg(r))
				code = 1
			}
		}()
		return gui.RunLoop(wb, func() {
			runErr := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = errors.New(enginePanicMsg(r))
					}
				}()
				return in.Run(prog)
			}()
			if runErr != nil {
				// Report to the terminal and terminate the process:
				// unlike a clean finish (the window stays open), an
				// error closes the window; the exit code below is 1.
				fmt.Fprintln(os.Stderr, "エラー:", runErr)
				wb.RequestClose()
			} else {
				wb.SetDone()
			}
			runCh <- runErr
		})
	}()
	var runErr error
	select {
	case runErr = <-runCh:
	default:
	}
	if runErr != nil {
		os.Exit(1)
	}
	if code, ok := in.ExitCode(); ok {
		os.Exit(code)
	}
	os.Exit(loopCode)
}

func cmdLex(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "lex はファイルを 1 つだけ指定してください")
		os.Exit(2)
	}
	toks, err := CombineFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	for _, t := range toks {
		fmt.Println(t.String())
	}
}

func cmdParse(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "parse はファイルを 1 つだけ指定してください")
		os.Exit(2)
	}
	prog, err := ParseFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	fmt.Print(prog.String())
}

func cmdRepl() {
	fmt.Println("Goulash v0.1 REPL (type \"exit\" to quit)")
	in := NewInterp(os.Stdout)
	// NOTE: the REPL reads through the interpreter Backend so input()
	// shares the same stdin reader instead of competing with it.
	var buf strings.Builder
	depth := 0
	prompt := "> "
	for {
		line, ok := in.be.ReadLine(prompt)
		if !ok {
			fmt.Println()
			return
		}
		trimmed := strings.TrimSpace(line)
		if buf.Len() == 0 && (trimmed == "exit" || trimmed == ":quit" || trimmed == ":q") {
			return
		}
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(line)
		src := buf.String()
		toks, err := CombineSource("", src, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "エラー:", err)
			buf.Reset()
			depth = 0
			prompt = "> "
			continue
		}
		depth = bracketDepth(toks)
		if depth > 0 {
			prompt = ">> "
			continue
		}
		if strings.TrimSpace(src) == "" {
			buf.Reset()
			prompt = "> "
			continue
		}
		prog, err := ParseTokens(toks)
		if err != nil {
			fmt.Fprintln(os.Stderr, "エラー:", err)
			buf.Reset()
			prompt = "> "
			continue
		}
		// Bare expressions echo their value; everything else just runs.
		if len(prog.Stmts) == 1 {
			if es, ok := prog.Stmts[0].(*ExprStmt); ok {
				v, err := in.EvalGlobal(es.X)
				if err != nil {
					fmt.Fprintln(os.Stderr, "エラー:", err)
				} else if v.K != KNull {
					fmt.Println(Stringify(v))
				}
				buf.Reset()
				prompt = "> "
				continue
			}
		}
		if err := in.Run(prog); err != nil {
			fmt.Fprintln(os.Stderr, "エラー:", err)
		}
		if code, ok := in.ExitCode(); ok {
			os.Exit(code)
		}
		buf.Reset()
		prompt = "> "
	}
}

// bracketDepth counts unclosed (, [, { in a token stream.
func bracketDepth(toks []Token) int {
	d := 0
	for _, t := range toks {
		switch t.Type {
		case TokLParen, TokLBracket, TokLBrace:
			d++
		case TokRParen, TokRBracket, TokRBrace:
			d--
		}
	}
	return d
}
