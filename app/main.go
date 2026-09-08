package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	builtInSet := map[string]string{ // keys are the recognised builtins; values are unused, only membership is checked (see handleTYPE)
		"type": "get cmd type",
		"echo": "print",
		"exit": "exiting",
		"pwd":  "get working directory",
		"cd":   "change directory",
	}

	shellLoop: // labeled so "exit" below can break out of the for loop, not just its switch
	for {
		fmt.Print("$ ")

		args, InputErr := handleInput(reader)

		if InputErr != nil {
			printLine(InputErr.Error())
			return
		}

		if len(args) == 0 {
			continue
		}

		cmdArgs, redirectTarget, redirErr , mode := extractRedirect(args)

		if redirErr != nil {
			printLine(redirErr.Error())
			continue
		}
		args = cmdArgs

		switch args[0] {
		case "exit":
			break shellLoop // exits the outer "for"; a bare break here would only exit this switch

		case "echo":
			cleanStr, _ := handleEcho(args) // handleEcho never errors
			if mode == 1 || mode == 3 {     // stdout redirect requested (truncate or append)
				writeOutput(redirectTarget, cleanStr,mode)
			} else {
				printLine(cleanStr)
			}

		case "pwd":
			dirName, err := handlePWD(args)

			if err != nil {
				if mode == 2 || mode == 4{ // stderr redirect requested
					writeError(redirectTarget, err,mode)
				} else {
					printLine(fmt.Sprintf("Error printing the working directory %s", err))
				}
				break
			}

			if mode == 1 || mode == 3{ // stdout redirect requested
				writeOutput(redirectTarget, dirName, mode)
			} else {
				printLine(dirName)
			}

		case "cd":
			errCD := handleCD(args) // cd has no stdout, so it isn't subject to output redirection

			if errCD != nil {
				printLine(errCD.Error())
			}

		case "type":
			typeString, err := handleTYPE(args, builtInSet)
			if err != nil {
				if mode == 2 || mode == 4{ // stderr redirect requested
					writeError(redirectTarget, err,mode)
				} else {
					printLine(err.Error())
				}
				break
			}
			if mode == 1 || mode == 3 { // stdout redirect requested
				writeOutput(redirectTarget, typeString,mode)
			} else {
				printLine(typeString)
			}

		default: // not a builtin: resolve and run as an external program
			msg, err := handleExecFile(args, redirectTarget, mode)

			if msg != "" || err != nil {
				printLine(err.Error())
			}

		}
	}
}
