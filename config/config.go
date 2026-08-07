package config

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"go.yaml.in/yaml/v3"
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

var DefaultDeployConfig = `servers:
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

func ValidateConfig(deployConfig DeployConfig) bool {
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

func fileToConfig[V any](configBytes []byte) (V, error) {
	var deployConfig V
	err := yaml.Unmarshal(configBytes, &deployConfig)
	if err != nil {
		return deployConfig, err
	}
	return deployConfig, err
}

type Action int

const (
	Init Action = iota
	Build
	Deploy
)

// parse configuration from cmd_line and config files and return structured output
func Parse() (DeployConfig, map[string]string, Action) {
	initSet := flag.NewFlagSet("init", flag.ExitOnError)
	cmdSet := flag.NewFlagSet("deploy", flag.ExitOnError)
	deployFile := cmdSet.String("f", "deploy.yml", "YAML file to deploy")
	secretsFile := cmdSet.String("secrets", ".secrets", "secrets file")

	if len(os.Args) < 2 {
		log.Fatal("insufficient params, please use --help to see available commands")
	}

	cmdSet.Parse(os.Args[1:])
	initSet.Parse(os.Args[1:])

	secrets, err := godotenv.Read(*secretsFile)
	if err != nil {
		fmt.Println("Warning secrets failed to load, deploy may not work as expected", err)
	}
	configBytes, err := os.ReadFile(*deployFile)
	if err != nil {
		log.Fatal("FATAL ERROR: Failed to read config file", err)
	}
	configExpanded := ExpandTemplate(string(configBytes), secrets)
	deployConfig, err := fileToConfig[DeployConfig]([]byte(configExpanded))
	if err != nil {
		log.Fatal("Failed to read deploy config", err)
	}

	var action Action

	switch os.Args[1] {
	case "init":
		action = Init
	case "build":
		action = Build
	case "deploy":
		action = Deploy
	default:
		log.Fatalf("please provide a valid parameter, use --help to see available commands")
	}

	return deployConfig, secrets, action
}

func InitConfigFile() {
	if _, err := os.Stat("deploy.yml"); os.IsNotExist(err) {
		file, err := os.OpenFile("deploy.yml", os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			log.Fatal(err)
		}
		file.Write([]byte(DefaultDeployConfig))
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
}
