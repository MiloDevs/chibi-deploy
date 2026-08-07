package builder

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/MiloDevs/chibi-deploy/config"
	"github.com/MiloDevs/chibi-deploy/handler"
	docker "github.com/fsouza/go-dockerclient"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func Build(deployConfig config.DeployConfig, secrets map[string]string) {
	client, _ := docker.NewClientFromEnv()

	for _, serviceConfig := range deployConfig.Services {
		BuildService(serviceConfig)
		if serviceConfig.Registry != "" {
			switch serviceConfig.Registry {
			case "ghcr":
				pushOpts := docker.PushImageOptions{
					Name:         serviceConfig.Image,
					Registry:     serviceConfig.Registry,
					OutputStream: os.Stdout,
					Tag:          "latest",
				}
				authConfig := docker.AuthConfiguration{
					Username:      secrets["GHCR_USER"],
					Password:      secrets["GH_TOKEN"],
					ServerAddress: "ghcr.io",
				}
				err := client.PushImage(pushOpts, authConfig)
				fmt.Println("Error pushing image:", err)
			default:
				fmt.Println("No such repository:", serviceConfig.Registry, "skipping")
			}
		}
	}
}

func Deploy(deployConfig config.DeployConfig, secrets map[string]string) {
	if len(deployConfig.DeployScript) != 0 {
		// if deploy script, run builds and simply ssh into server and execute script
		for serverName, serverConfig := range deployConfig.Servers {
			fullHost := fmt.Sprintf("%s:%d", serverConfig.Host, serverConfig.Port)
			fmt.Println("Connecting to server", serverName, fullHost)

			// read known hosts
			homeDir, _ := os.UserHomeDir()
			hostKeyCallback, err := knownhosts.New(fmt.Sprintf("%s/.ssh/known_hosts", homeDir))
			if err != nil {
				log.Fatal("Could not load known_hosts file:", err)
			}

			key, err := os.ReadFile(serverConfig.SshKey)
			if err != nil {
				fmt.Println("Error retrieving ssh key:", err)
				os.Exit(1)
			}
			signer, err := ssh.ParsePrivateKey(key)
			if err != nil {
				fmt.Println("Error parsing private key:", err)
			}
			algorithms := ssh.SupportedAlgorithms()
			config := &ssh.ClientConfig{
				Config: ssh.Config{
					KeyExchanges: algorithms.KeyExchanges,
					Ciphers:      algorithms.Ciphers,
					MACs:         algorithms.MACs,
				},
				User: serverConfig.User,
				Auth: []ssh.AuthMethod{
					ssh.PublicKeys(signer),
				},
				HostKeyCallback:   hostKeyCallback,
				HostKeyAlgorithms: algorithms.HostKeys,
			}
			client, err := ssh.Dial("tcp", fullHost, config)
			if err != nil {
				log.Fatal("Failed to dial:", err)
			}
			defer client.Close()

			session, err := client.NewSession()
			if err != nil {
				log.Fatal("Couldn't start a new session:", err)
			}
			defer session.Close()

			var scriptBuilder strings.Builder
			for key, val := range secrets {
				scriptBuilder.WriteString("export ")
				scriptBuilder.WriteString(key)
				scriptBuilder.WriteString("=\"")
				scriptBuilder.WriteString(val)
				scriptBuilder.WriteString("\"\n")
			}

			session.Stdout = os.Stdout
			session.Stderr = os.Stderr

			scriptBuilder.WriteString(deployConfig.DeployScript)

			fmt.Printf("Running deploy script on %s...\n", serverName)
			err = session.Run(scriptBuilder.String())
			if err != nil {
				log.Fatal("Failed to run command: ", err)
			}
		}
	}
}

func BuildService(deployConfig config.ServiceConfig) {
	client, err := docker.NewClientFromEnv()
	handler.CheckError(err)

	// get file content ignoring files in workdir
	buildContext, err := CreateBuildContext(deployConfig.SrcDir)
	if err != nil {
		log.Fatalf("Failed to initialize build context: %v", err)
	}

	opts := docker.BuildImageOptions{
		Name:         deployConfig.Image,
		InputStream:  buildContext,
		OutputStream: os.Stdout,
	}

	if err := client.BuildImage(opts); err != nil {
		log.Fatal(err)
	}
}
