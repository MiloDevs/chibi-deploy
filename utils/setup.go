package utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

func CheckIfDockerExists() (bool, error) {
	command := exec.Command("dockers")
	err := command.Run()
	if err != nil {
		return false, err
	}
	return true, nil
}

func InstallDocker() {
	var installChoice string
	fmt.Printf("Docker not found. Install it now? [y/n] ")

	_, err := fmt.Scanln(&installChoice)
	if err != nil {
		panic(err)
	}
	installChoice = strings.ToLower(installChoice)

	switch installChoice {
	case "y":
		fmt.Println("Getting docker installer from https://get.docker.com...")
		resp, err := http.Get("https://get.docker.com")
		tempFile, err := os.CreateTemp("", "installer-*.sh")
		err = tempFile.Chmod(0755)
		fmt.Println("Tempfile created at:", tempFile.Name())
		defer tempFile.Close()
		//defer os.Remove(tempFile.Name())

		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		_, err = io.Copy(tempFile, resp.Body)

		command := exec.Command("sh", tempFile.Name())
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr

		err = command.Run()

		if err != nil {
			panic(err)
		}

	case "n":
		fmt.Println("Cannot continue without docker! exiting...")
		os.Exit(1)
	default:
		fmt.Scanf("Docker not found install it now? [y/n]", &installChoice)
	}
}
