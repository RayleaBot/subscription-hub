package main

import (
	"github.com/RayleaBot/RayleaBot/sdk/go/pluginbuild"
	"github.com/RayleaBot/RayleaBot/sdk/go/pluginbuild/buildcmd"
)

func main() {
	buildcmd.Main(buildcmd.Config{
		BackendPackage: "./cmd/subscription-hub",
		Assets:         []string{"templates"},
		MappedAssets: []pluginbuild.AssetMapping{{
			Source: "internal/assets/default_config.json", Destination: "default_config.json",
		}},
	})
}
