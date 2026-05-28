package main

import (
	"archive/tar"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/joho/godotenv"
	"github.com/moby/patternmatcher"
	"go.yaml.in/yaml/v3"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type ServerConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	SshKey   string `yaml:"ssh_key"`
}

type DependencyConfig struct {
	Image        string `yaml:"image"`
	PublishPorts bool   `yaml:"publish_ports"`
	Volumes      bool   `yaml:"volumes"`
}

type ServiceConfig struct {
	Image        string `yaml:"image"`
	SrcDir       string `yaml:"src_dir"`
	Registry     string `yaml:"registry"`
	Dependencies map[string]DependencyConfig
}

type DeployConfig struct {
	Servers      map[string]ServerConfig  `yaml:"servers"`
	Services     map[string]ServiceConfig `yaml:"services"`
	DeployScript string                   `yaml:"deploy_script"`
}

func check_error(err error) {
	if err != nil {
		panic(err)
	}
}

func validateConfig(deployConfig DeployConfig) bool {
	if len(deployConfig.Servers) == 0 {
		log.Fatal("Servers must be defined!")
	}
	for serverName, serverConfig := range deployConfig.Servers {
		if serverConfig.Host == "" {
			log.Fatalf("Host not defined for server: %s", serverName)
		}
		if serverConfig.Port == 0 {
			log.Fatalf("Port not defined for server: %s", serverName)
		}
		if serverConfig.User == "" {
			log.Fatalf("User not defined for server: %s", serverName)
		}
		if serverConfig.Password == "" && serverConfig.SshKey == "" {
			log.Fatalf("Password not defined for server (one password or sshkey is required): %s", serverName)
		}
		if serverConfig.Host == "" {
			log.Fatalf("SSHKey not defined for server: %s", serverName)
		}
	}
	return true
}

func CreateBuildContext(contextDir string) (io.Reader, error) {
	excludes, err := parseDockerignore(contextDir)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)
		defer func() {
			tw.Close()
			pw.Close()
		}()

		err := filepath.Walk(contextDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			cleanRelPath := filepath.Join(contextDir, path)

			isMatch, err := patternmatcher.Matches(cleanRelPath, excludes)
			if err != nil {
				return err
			}

			if isMatch {
				return nil
			}

			fi, err := os.Lstat(path)

			var link string
			if fi.Mode()&os.ModeSymlink != 0 {
				link, err = os.Readlink(path)
				if err != nil {
					return err
				}
			}

			header, err := tar.FileInfoHeader(fi, link)
			if err != nil {
				return err
			}

			header.Name = filepath.ToSlash(cleanRelPath)
			if fi.IsDir() && !strings.HasSuffix(header.Name, "/") {
				header.Name += "/"
			}
			header.Format = tar.FormatPAX
			header.AccessTime = time.Time{}
			header.ChangeTime = time.Time{}

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			// Only copy content for regular files, not symlinks or dirs
			if fi.Mode().IsRegular() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = io.Copy(tw, file)
				return err
			}
			return nil
		})

		if err != nil {
			pw.CloseWithError(err)
		}
	}()

	return pr, nil
}

// Internal helper to read and tokenize the .dockerignore file lines
func parseDockerignore(root string) ([]string, error) {
	var excludes []string
	ignorePath := filepath.Join(root, ".dockerignore")

	data, err := os.ReadFile(ignorePath)
	if os.IsNotExist(err) {
		return excludes, nil
	} else if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		excludes = append(excludes, filepath.Clean(line))
	}
	return excludes, nil
}

func buildService(deployConfig ServiceConfig) {
	client, err := docker.NewClientFromEnv()
	check_error(err)

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

var defaultDeployConfig = `servers:
  prod:
    host: server-host
    port: server-port default 22
    user: server-user default root (recommended for sudo commands)
    ssh_key: ~/path-to-ssh-key
services:
  api:
    image: ghcr.io/repo-owner/image:tag // use only image:tag for docker hub otherwise ghcr.io/repo-owner/image:tag for github registry
    src_dir: ./source-path-with-dockerfile
    registry: ghcr
    dependencies:
      db:
        image: mysql:latest
        publish_ports: true
        volumes: true
deploy_script: |
  // stuff to do on your server`

func main() {
	initSet := flag.NewFlagSet("init", flag.ExitOnError)
	cmdSet := flag.NewFlagSet("deploy", flag.ExitOnError)
	deployFile := cmdSet.String("f", "deploy.yml", "YAML file to deploy")
	secretsFile := cmdSet.String("secrets", ".secrets", "secrets file")
	client, _ := docker.NewClientFromEnv()

	if len(os.Args) < 2 {
		log.Fatal("insufficient params, please use --help to see available commands")
	}

	cmdSet.Parse(os.Args[1:])
	initSet.Parse(os.Args[1:])

	switch os.Args[1] {
	case "init":
		if _, err := os.Stat("deploy.yml"); os.IsNotExist(err) {
			file, err := os.OpenFile("deploy.yml", os.O_RDWR|os.O_CREATE, 0666)
			if err != nil {
				log.Fatal(err)
			}
			file.Write([]byte(defaultDeployConfig))
			file.Close()
		} else {
			fmt.Println("deploy.yml already exists, skipping init")
		}
		if _, err := os.Stat(".secrets"); os.IsNotExist(err) {
			file, err := os.OpenFile(".secrets", os.O_RDWR|os.O_CREATE, 0666)
			if err != nil {
				log.Fatal(err)
			}
			file.Close()
		} else {
			fmt.Println("secrets already exists, skipping init")
		}
		fmt.Println("init successful!")
	case "deploy":
		secrets, err := godotenv.Read(*secretsFile)
		if err != nil {
			fmt.Println("Warning secrets failed to load, deploy may not work as expected", err)
		}

		yamlBytes, err := os.ReadFile(*deployFile)
		check_error(err)

		var deployConfig DeployConfig
		err = yaml.Unmarshal(yamlBytes, &deployConfig)

		// validate deploy config
		validateConfig(deployConfig)
		// TODO: Implement build step
		for _, serviceConfig := range deployConfig.Services {
			buildService(serviceConfig)
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

		// deploy
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
		} else {

		}
	}
}
