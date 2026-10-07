package main

import (
	"context"
	"fmt"
	"os"

	"github.com/RayleaBot/subscription-hub/internal/platforms/bilibili"
	"github.com/RayleaBot/subscription-hub/internal/platforms/douyin"
	"github.com/RayleaBot/subscription-hub/internal/platforms/netease_music"
	"github.com/RayleaBot/subscription-hub/internal/platforms/weibo"
	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

func main() {
	if err := plugin.Run(context.Background(), platforms()...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func platforms() []plugin.Platform {
	return []plugin.Platform{bilibili.New(), weibo.New(), douyin.New(), netease_music.New()}
}
