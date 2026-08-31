package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

type resolverVideoActions struct {
	*testkit.Actions
	fileUploadActions []string
}

func (actions *resolverVideoActions) Call(_ context.Context, action string, _ any, _ any) error {
	actions.fileUploadActions = append(actions.fileUploadActions, action)
	return nil
}

func TestSendResolverVideoKeepsDouyinAsVideo(t *testing.T) {
	videoPath := filepath.Join(t.TempDir(), "fixture.mp4")
	file, err := os.Create(videoPath)
	if err != nil {
		t.Fatalf("create fixture video: %v", err)
	}
	if err := file.Truncate(2 << 20); err != nil {
		_ = file.Close()
		t.Fatalf("size fixture video: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close fixture video: %v", err)
	}

	tests := []struct {
		name           string
		platform       string
		wantVideo      bool
		wantFileAction string
	}{
		{name: "douyin stays video", platform: "douyin", wantVideo: true},
		{name: "other platform keeps oversize strategy", platform: "bilibili", wantFileAction: "file.group.upload"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &resolverVideoActions{Actions: testkit.NewActions()}
			err := sendPreparedResolverMedia(
				context.Background(),
				actions,
				test.platform,
				[]preparedResolverMedia{{Kind: "video", Path: videoPath, Name: "fixture.mp4"}},
				ResolverMediaSettings{UploadOversize: true, VideoSizeLimitMB: 1},
				resolverSendTarget{TargetType: "group", TargetID: "12345"},
			)
			if err != nil {
				t.Fatalf("sendPreparedResolverMedia() error = %v", err)
			}
			if test.wantVideo {
				if len(actions.Messages) != 1 || len(actions.Messages[0].Message.Segments) != 1 || actions.Messages[0].Message.Segments[0].Type != "video" {
					t.Fatalf("douyin delivery = messages %#v, file actions %#v", actions.Messages, actions.fileUploadActions)
				}
				if len(actions.fileUploadActions) != 0 {
					t.Fatalf("douyin unexpectedly used file upload: %#v", actions.fileUploadActions)
				}
				return
			}
			if len(actions.Messages) != 0 || len(actions.fileUploadActions) != 1 || actions.fileUploadActions[0] != test.wantFileAction {
				t.Fatalf("oversize delivery = messages %#v, file actions %#v", actions.Messages, actions.fileUploadActions)
			}
		})
	}
}
