package main

import (
	"github.com/MiloDevs/chibi-deploy/builder"
	"github.com/MiloDevs/chibi-deploy/config"
)

func main() {
	deployConfig, secrets, action := config.Parse()
	// validate config
	config.ValidateConfig(deployConfig)
	// setu
	switch action {
	case config.Init:
		config.InitConfigFile()
	case config.Build:
		builder.Build(deployConfig, secrets)
	}
}
