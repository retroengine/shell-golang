package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"io/fs"
	"os/exec"
	"strings"
)

func handleInput(reader *bufio.Reader) ([]string , error) {
	line , err := reader.ReadString('\n')

	if err != nil {
		return  nil , fmt.Errorf("Unable to read Input")
	}

	line = strings.TrimSpace(line)
	args := strings.Split(line, " ")

	return args , nil
}

func handleEcho(srgs []string) ()
func main() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("$ ")

		args , InputErr := handleInput(reader)

		if InputErr != nil {
			fmt.Print(InputErr)
		}

		switch args[0] {
		case "exit":
			break
		case "echo":
			handleEcho(args)
		case "pwd":
			handlePWD(args)
		case "cd":
			handleCD(args)
		case "type":
			handleTYPE(args)
		case "execFile":
			handleEXECFILE(args)
		default:

		}
	}
}