package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// extractRedirect pulls a trailing redirect operator out of args; mode is 1=stdout truncate, 2=stderr truncate, 3=stdout append, 4=stderr append.
func extractRedirect(args []string) ([]string, string, error, int) {
	for i, a := range args {
		mode := 0
		switch a {
		case ">", "1>":
			mode = 1
		case "2>":
			mode = 2
		case ">>", "1>>":
			mode = 3
		case "2>>":
			mode = 4
		}
		if mode == 0 {
			continue
		}
		if i+1 >= len(args) {
			return nil, "", fmt.Errorf("syntax error: expected file after %s", a), 0
		}
		cleaned := append(append([]string{}, args[:i]...), args[i+2:]...)
		return cleaned, args[i+1], nil, mode
	}
	return args, "", nil, 0
}

// stdoutIsTerm is true only when stdout is a real console, not a pipe (as in the e2e tests, which expect plain "\n" output).
var stdoutIsTerm = term.IsTerminal(int(os.Stdout.Fd()))

// printLine prints s plus "\r\n" (carriage return + line feed) on a real terminal, or plain "\n" otherwise — Windows doesn't translate a bare "\n" and every line staircases right without this.
func printLine(s string) {
	if stdoutIsTerm {
		fmt.Print(s + "\r\n")
	} else {
		fmt.Println(s)
	}
}

func writeOutput(target, s string , mode int) error {

	if target == "" {
		printLine(s)
		return nil
	}

	if mode == 1 { // truncate
		return os.WriteFile(target, []byte(s+"\n"), 0644)
	} else { // append
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

		if err != nil {
			return err
		}
		defer f.Close()

		_ , err = f.WriteString(s + "\n")

		if err != nil {
			return err
		}
	}

	return nil
}

func writeError(target string, err error,mode int) error {
	if err == nil {
		return nil
	}

	if target == "" {
		printLine(err.Error())
		return nil
	}

	if mode == 2 { // truncate
		return os.WriteFile(target, []byte(err.Error()+"\n"), 0644)
	} else { // append
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

		if err != nil {
			return err
		}
		defer f.Close()

		_ , err = f.WriteString(err.Error() + "\n")

		if err != nil {
			return err
		}
	}

	return nil

}
