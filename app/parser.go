package main

import (
	"bufio"
	"fmt"
	"strings"
)

// handleInput reads one line and tokenizes it into arguments, resolving single quotes, double quotes, and backslash escapes.
func handleInput(reader *bufio.Reader) ([]string, error) {
	line, err := readLine(reader)

	if err != nil {
		return nil, fmt.Errorf("Unable to read Input")
	}

	line = strings.TrimSpace(line)

	var args []string
	var current strings.Builder // the argument currently being built, one rune at a time
	inArg := false              // true once current holds a rune, or was opened by an (even empty) quote
	inQuote := false            // inside '...': every character is literal until the closing '
	inDoubleQuote := false      // inside "...": literal except for \" and \\
	slash := false              // previous rune outside quotes was a backslash: keep whatever comes next as-is
	pendingEscape := false      // previous rune inside double quotes was a backslash: resolve it against this rune

	for _, r := range line {
		switch {
		case slash: // finish an outside-quotes escape: the escaped char is always kept literally
			current.WriteRune(r)
			slash = false
			inArg = true
		case inQuote: // single-quoted text: only a closing ' has meaning
			if r == '\'' {
				inQuote = false
			} else {
				current.WriteRune(r)
			}

		case inDoubleQuote:
			switch {
			case pendingEscape: // finish a double-quote escape started by the previous rune
				pendingEscape = false
				if r == '"' || r == '\\' {
					current.WriteRune(r) // \" and \\ collapse to a single literal character
				} else {
					current.WriteRune('\\')
					current.WriteRune(r) // any other \x keeps both the backslash and the character
				}
			case r == '"':
				inDoubleQuote = false // closing double quote
			case r == '\\':
				pendingEscape = true // defer the decision to the next rune
			default:
				current.WriteRune(r)
			}

		case r == '\'':
			inQuote = true // opening single quote
			inArg = true
		case r == '"':
			inDoubleQuote = true // opening double quote
			inArg = true
		case r == '\\' && !inQuote && !inDoubleQuote:
			slash = true // start an outside-quotes escape

		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, current.String()) // unquoted whitespace ends the current argument
				current.Reset()
				inArg = false
			}
		default:
			current.WriteRune(r) // ordinary character
			inArg = true
		}
	}

	if inArg {
		args = append(args, current.String()) // flush the final argument; no trailing whitespace triggers the case above
	}

	return args, nil
}
