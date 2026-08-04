package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/RayleaBot/RayleaBot/sdk/go/pluginbuild"
)

func main() {
	target := flag.String("target", pluginbuild.CurrentPlatform(), "target platform")
	output := flag.String("out", "dist", "artifact output directory")
	flag.Parse()
	result, err := pluginbuild.Build(context.Background(), pluginbuild.Config{
		PluginDir: ".", OutputDir: *output, TargetPlatform: *target,
		Assets: []string{"default_config.json", "templates"}, KeepExpandedArtifact: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(result.ArchivePath)
}
