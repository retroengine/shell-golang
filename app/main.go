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

func handleEcho(args []string) (string , error) {
	cleanStr := strings.Join(args[1:] , " ")

	cleanStr = strings.ReplaceAll(cleanStr, "'","")

	return cleanStr , nil;
}

func handlePWD(args []string) (string , error) {
	dir , err := os.Getwd()

	if err != nil {
		return "",err
	}

	return dir,nil
}

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

			cleanStr , _ := handleEcho(args)
			fmt.Println(cleanStr)

		case "pwd":
			dirName , err := handlePWD(args)

			if err != nil {
				fmt.Printf("Error printing the working directory %s",err)
			}

			fmt.Println(dirName)

		case "cd":
			handleCD(args)
		case "type":
			handleTYPE(args)
		case "execFile":
			handleEXECFILE(args)
		default:

		}
		
		fmt.Println()
	}
}