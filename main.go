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
	"fmt"
	"os"
	"strings"
)

const usage = `gsh: Goulash v0.1 インタプリタ

使い方:
  gsh run <file.gsh> [--keep] [--gui] [-- args...]   スクリプトを実行します
  gsh repl                               対話環境（REPL）を起動します
  gsh lex <file.gsh>                    字句トークン列を出力します（デバッグ用）
  gsh parse <file.gsh>                  構文木（AST）を出力します（デバッグ用）

--gui はウィンドウを開きます（-tags gui ビルドが必要です）。
--keep は将来の互換性のために予約されており、現在は何も行いません。`

// GUI hooks, wired by guihook_gui.go under -tags gui.
var newGUIBackend func(w, h int) (Backend, error)
var runGUILoop func(be Backend, run func()) int
var guiSetDone func(be Backend)

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
	guiMode := false
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
			guiMode = true
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
	prog, err := ParseFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	if guiMode {
		runGUI(file, prog, scriptArgs)
		return
	}
	in := NewInterp(os.Stdout)
	in.SetArgs(scriptArgs)
	if err := in.Run(prog); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	if code, ok := in.ExitCode(); ok {
		os.Exit(code)
	}
}

// runGUI executes prog in a window. It requires a -tags gui build.
func runGUI(file string, prog *Program, scriptArgs []string) {
	_ = file
	if newGUIBackend == nil || runGUILoop == nil {
		fmt.Fprintln(os.Stderr, "エラー: --gui は GUI ビルドが必要です（go run -tags gui . run "+file+"）")
		os.Exit(2)
	}
	be, err := newGUIBackend(640, 480)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
	in := NewInterpWithBackend(be, os.Stdin)
	in.SetArgs(scriptArgs)
	var runErr error
	loopCode := runGUILoop(be, func() {
		runErr = in.Run(prog)
		if runErr != nil {
			// Report immediately (verifiable even if the window is killed)
			// and also inside the window, which stays open for inspection.
			fmt.Fprintln(os.Stderr, "エラー:", runErr)
			be.SetColor(255, 90, 90)
			be.Println("エラー: " + runErr.Error())
			be.ResetColor()
		} else {
			guiSetDone(be)
		}
	})
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
