package main

import (
	"bufio"
	"fmt"
	"os"
)
var jobsCount int = 0

func main() {
	reader := bufio.NewReader(os.Stdin)

	

	builtInSet := map[string]string{ // keys are the recognised builtins; values are unused, only membership is checked (see handleTYPE)
		"type": "get cmd type",
		"echo": "print",
		"exit": "exiting",
		"pwd":  "get working directory",
		"cd":   "change directory",
		"complete": "registers autocompletion for given word",
		"jobs":"to identify the bg task and more",
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

		if len(args) == 0 { // a redirect with no command in front of it: create or truncate the target, run nothing
			if err := touchTarget(redirectTarget, mode); err != nil {
				printLine(err.Error())
			}
			continue
		}

		jobArg := false

		if args[len(args)-1] == "&" {
			jobArg = true
			args = args[:len(args)-1]
		}

		if len(args) == 0 {
			continue
		}

		if jobArg {
			jobsCount++
		}

		switch args[0] {
		case "exit":
			break shellLoop // exits the outer "for"; a bare break here would only exit this switch

		case "echo":
			cleanStr, _ := handleEcho(args) // handleEcho never errors
			if mode == 1 || mode == 3 {     // stdout redirect requested (truncate or append)
				if werr := writeOutput(redirectTarget, cleanStr, mode); werr != nil {
					printLine(werr.Error())
				}
			} else {
				printLine(cleanStr)
			}

		case "pwd":
			dirName, err := handlePWD(args)

			if err != nil {
				if mode == 2 || mode == 4{ // stderr redirect requested
					if werr := writeError(redirectTarget, err, mode); werr != nil {
						printLine(werr.Error())
					}
				} else {
					printLine(fmt.Sprintf("Error printing the working directory %s", err))
				}
				break
			}

			if mode == 1 || mode == 3{ // stdout redirect requested
				if werr := writeOutput(redirectTarget, dirName, mode); werr != nil {
					printLine(werr.Error())
				}
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
				if mode == 2 || mode == 4 { // stderr redirect requested
					if werr := writeError(redirectTarget, err, mode); werr != nil {
						printLine(werr.Error())
					}
				} else {
					printLine(err.Error())
				}
				break
			}
			if mode == 1 || mode == 3 { // stdout redirect requested
				if werr := writeOutput(redirectTarget, typeString, mode); werr != nil {
					printLine(werr.Error())
				}
			} else {
				printLine(typeString)
			}

		case "complete":
			strComplete := handleComplete(args)

			if strComplete != "" {
				printLine(strComplete)
			}

		default: // not a builtin: resolve and run as an external program
			msg, err := handleExecFile(args, redirectTarget, mode,jobArg)

			if err != nil {
				printLine(err.Error())
			} else if msg != "" {
				printLine(msg)
			}

		}
	}
}
