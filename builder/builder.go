package builder

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/MiloDevs/chibi-deploy/config"
	"github.com/MiloDevs/chibi-deploy/handler"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func Build(deployConfig config.DeployConfig, secrets map[string]string) {
	dockerClient, _ := client.New(client.FromEnv)

	for _, serviceConfig := range deployConfig.Services {
		BuildService(serviceConfig)
		if serviceConfig.Registry != "" {
			switch serviceConfig.Registry {
			case "ghcr":
				authConfig := registry.AuthConfig{
					Username:      secrets["GHCR_USER"],
					Password:      secrets["GH_TOKEN"],
					ServerAddress: "ghcr.io",
				}
				encodedJSON, err := json.Marshal(authConfig)
				if err != nil {
					log.Printf("Error encoding auth config: %v\n", err)
					continue
				}
				authStr := base64.URLEncoding.EncodeToString(encodedJSON)
				clientPushOpts := client.ImagePushOptions{
					RegistryAuth: authStr,
				}
				imageRef := fmt.Sprintf("%s:latest", serviceConfig.Image)
				reader, err := dockerClient.ImagePush(context.Background(), imageRef, clientPushOpts)
				if err != nil {
					fmt.Println("Error pushing image:", err)
					continue
				}
				defer reader.Close()

				// Stream push output to Stdout
				_, _ = os.Stdout.ReadFrom(reader)

				fmt.Println("Error pushing image:", err)
			default:
				fmt.Println("No such repository:", serviceConfig.Registry, "skipping")
			}
		}
	}
}

func sshConnect(hostname string, user string, port int, key string) (*ssh.Client, error) {
	fullHost := fmt.Sprintf("%s:%d", hostname, port)

	homeDir, _ := os.UserHomeDir()
	hostKeyCallback, err := knownhosts.New(fmt.Sprintf("%s/.ssh/known_hosts", homeDir))
	if err != nil {
		log.Fatal("Could not load known_hosts file:", err)
	}

	keyBytes, err := os.ReadFile(key)
	if err != nil {
		keyBytes = []byte(key)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		fmt.Println("Error parsing private key:", err)
		return nil, err
	}
	algorithms := ssh.SupportedAlgorithms()
	config := &ssh.ClientConfig{
		Config: ssh.Config{
			KeyExchanges: algorithms.KeyExchanges,
			Ciphers:      algorithms.Ciphers,
			MACs:         algorithms.MACs,
		},
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: algorithms.HostKeys,
	}
	client, err := ssh.Dial("tcp", fullHost, config)
	if err != nil {
		log.Fatal("Failed to dial:", err)
		return nil, err
	}
	return client, nil
}

func Deploy(deployConfig config.DeployConfig, secrets map[string]string) {
	ctx := context.Background()

	// if deploy script exists ignore normal deploy logic
	if len(deployConfig.DeployScript) != 0 {
		// if deploy script, run builds and simply ssh into server and execute script
		for serverName, serverConfig := range deployConfig.Servers {
			client, err := sshConnect(serverConfig.Host, serverConfig.User, serverConfig.Port, serverConfig.SshKey)
			if err != nil {
				fmt.Println("Couldn't connect to server:", serverName)
				continue
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
		return
	}
	// 1. iterate over servers
	for serverName, serverConfig := range deployConfig.Servers {
		sshClient, err := sshConnect(serverConfig.Host, serverConfig.User, serverConfig.Port, serverConfig.SshKey)
		if err != nil {
			fmt.Println("Couldn't connect to server:", serverName)
			continue
		}
		defer sshClient.Close()

		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					remoteConn, err := sshClient.Dial("unix", "/var/run/docker.sock")
					if err != nil {
						sshClient.Close()
						return nil, fmt.Errorf("failed to dial remote docker socker: %w", err)
					}
					return remoteConn, nil
				},
			},
		}

		clientOpts := []client.Opt{
			client.WithHost("https://docker"),
			client.WithHTTPClient(httpClient),
		}

		dockerClient, err := client.New(clientOpts...)
		if err != nil {
			log.Fatal(err)
		}
		for serviceName, serviceConfig := range deployConfig.Services {
			if strings.Contains(serviceConfig.Image, "ghcr") {
				// init ghcr auth
			}
			imageRef := fmt.Sprintf("%s:latest", serviceConfig.Image)
			imagePullResponse, err := dockerClient.ImagePull(ctx, imageRef, client.ImagePullOptions{})
			if err != nil {
				fmt.Println("Error pulling image:", imageRef, err)
			}
			_, _ = os.Stdout.ReadFrom(imagePullResponse)
			container, _ := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
				Name:   serviceName,
				Image:  imageRef,
				Config: &container.Config{},
			})
			containerStart, _ := dockerClient.ContainerStart(ctx, container.ID, client.ContainerStartOptions{})
			fmt.Println(containerStart)
		}

	}
}

func BuildService(deployConfig config.ServiceConfig) {
	dockerClient, err := client.New(client.FromEnv)
	handler.CheckError(err)

	// get file content ignoring files in workdir
	buildContext, err := CreateBuildContext(deployConfig.SrcDir)
	if err != nil {
		log.Fatalf("Failed to initialize build context: %v", err)
	}

	opts := client.ImageBuildOptions{
		Tags: []string{deployConfig.Image},
	}

	res, err := dockerClient.ImageBuild(context.Background(), buildContext, opts)

	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()

	_, _ = os.Stdout.ReadFrom(res.Body)
}
