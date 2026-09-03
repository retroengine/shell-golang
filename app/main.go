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

	var args []string
	var current strings.Builder
	inArg := false
	inQuote := false
	inDoubleQuote := false

	for _, r := range line {
		switch {
		case inQuote:
			if r == '\'' {
				inQuote = false
			} else {
				current.WriteRune(r)
			}
			
		case inDoubleQuote:
			if r == '"' {
				inDoubleQuote = false
			} else {
				current.WriteRune(r)
			}

		case r == '\'':
				inQuote = true
				inArg = true
			
		case r == '"' :
				inDoubleQuote = true;
				inArg  = true

		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, current.String())
				current.Reset()
				inArg = false
			}
		default:
			current.WriteRune(r)
			inArg = true
		}
	}

	if inArg {
		args = append(args, current.String())
	}

	return args , nil
}

func handleEcho(args []string) (string , error) {
	if len(args) == 0 {
		return "" , nil
	}

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

func handleCD(args []string) (error) {
	if len(args) < 2 {
		return fmt.Errorf("cd: missing operand")
	}

	if args[1] == "~" {
		homePath := os.Getenv("HOME")
		err := os.Chdir(homePath)
		if errors.Is(err,fs.ErrNotExist) {
			return fmt.Errorf("cd: %s: No such home directory",homePath)
		}
		if err != nil {
			return err
		}
	} else {
		err := os.Chdir(args[1])
		if errors.Is(err,fs.ErrNotExist) {
			return fmt.Errorf("cd: %s: No such home directory",args[1])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func handleTYPE(args []string,builtInSet map[string]string) (string, error) {
	if len(args) == 1 {
		return "No args provided" , nil
	}

	_ , ok := builtInSet[args[1]]

	if ok {
		return fmt.Sprintf("%s is a shell builtin",args[1]) , nil
	} else {
		pathAns , err := exec.LookPath(args[1])

		if errors.Is(err , exec.ErrNotFound) {
			return fmt.Sprintf("%s: not found",args[1]), nil
		}

		if err != nil {
			return "Error while visiting file" , err
		}

		return fmt.Sprintf("%s is %s",args[1],pathAns) , nil
	}
}

func handleExecFile(args []string) (string,error) {
	if len(args) == 0 {
		return "" , fmt.Errorf("no command provided")
	}

	_ , err := exec.LookPath(args[0])

	if errors.Is(err,exec.ErrNotFound) {
		return fmt.Sprintf("%s: command not found",args[0]), err
	}

	if err != nil {
		return "Error while visiting file" , err
	}

	cmd := exec.Command(args[0],args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin 

	err_cmd := cmd.Run()

	if err_cmd != nil {
		return "Error while executing file." , err_cmd
	}

	return "" , nil

}

func main() {
	reader := bufio.NewReader(os.Stdin)

	builtInSet := map[string]string{
		"type":"get cmd type" ,
		"echo":"print" ,
		"exit":"exiting" ,
		"pwd":"get working directory",
		"cd":"change directory",
	}

	for {
		fmt.Print("$ ")

		args , InputErr := handleInput(reader)

		if InputErr != nil {
			fmt.Println(InputErr)
			return
		}

		if len(args) == 0 {
			fmt.Println()
			continue
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
			errCD := handleCD(args)

			if errCD != nil {
				fmt.Print(errCD)
			}

		case "type":
			typeString , _ := handleTYPE(args,builtInSet)

			fmt.Print(typeString)

		default:
			msg , err := handleExecFile(args)

			if msg != "" || err != nil {
				fmt.Print(err)
			}

		}

		fmt.Println()
	}
}