package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	resolverImageMaxBytes = 64 << 20
	resolverAudioMaxBytes = 128 << 20
	resolverVideoMaxBytes = 2 << 30
)

type preparedResolverMedia struct {
	Kind string
	Path string
	Name string
}

func (handler *Handler) deliverResolverMedia(ctx context.Context, event *rayleabot.EventContext, platform string, update Update, plan ResolverMediaPlan, settings ResolverMediaSettings) error {
	tempRoot, err := os.MkdirTemp("", "raylea-resolver-*")
	if err != nil {
		return fmt.Errorf("创建临时目录：%w", err)
	}
	defer os.RemoveAll(tempRoot)

	prepared := make([]preparedResolverMedia, 0, len(plan.Sources))
	audioCache := map[string]string{}
	for index, source := range plan.Sources {
		item, err := prepareResolverMediaSource(ctx, tempRoot, index, source, settings, audioCache)
		if err != nil {
			return err
		}
		prepared = append(prepared, item)
	}
	if len(prepared) == 0 {
		return nil
	}
	if len(prepared) == 1 && prepared[0].Kind == "video" {
		return handler.sendResolverVideo(ctx, event, prepared[0], settings)
	}
	allImages := true
	for _, item := range prepared {
		allImages = allImages && item.Kind == "image"
	}
	if allImages && settings.ImageForwardThreshold > 0 && len(prepared) <= settings.ImageForwardThreshold {
		segments := make([]rayleabot.Segment, 0, len(prepared))
		for _, item := range prepared {
			segments = append(segments, rayleabot.Image(item.Path))
		}
		_, err := handler.hostActions(event).MessageSend(ctx, rayleabot.MessageSendRequest{
			TargetType: NormalizedTargetType(event.Event.Target.Type), TargetID: event.Event.Target.ID,
			Message: rayleabot.MessageOut{Segments: segments},
		})
		return err
	}
	return sendResolverForward(ctx, handler.hostActions(event), event, platform, prepared, settings.ImageBatchSize)
}

func prepareResolverMediaSource(ctx context.Context, tempRoot string, index int, source ResolverMediaSource, settings ResolverMediaSettings, audioCache map[string]string) (preparedResolverMedia, error) {
	name := safeResolverFileName(source.FileName, source.Kind, index)
	outputPath := filepath.Join(tempRoot, name)
	if source.Kind == "live" {
		if len(source.URLs) == 0 {
			return preparedResolverMedia{}, errors.New("直播流地址为空")
		}
		outputPath = strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".mp4"
		if err := recordResolverLive(ctx, source.URLs[0], source.Headers, settings.LiveRecordSeconds, outputPath, settings.CompatibilityTranscode); err != nil {
			return preparedResolverMedia{}, err
		}
		return preparedResolverMedia{Kind: "video", Path: outputPath, Name: filepath.Base(outputPath)}, nil
	}
	maximum := int64(resolverVideoMaxBytes)
	if source.Kind == "image" {
		maximum = resolverImageMaxBytes
	}
	if err := downloadResolverFile(ctx, source.URLs, source.Headers, outputPath, maximum); err != nil {
		return preparedResolverMedia{}, err
	}
	if source.Kind == "image" {
		return preparedResolverMedia{Kind: "image", Path: outputPath, Name: filepath.Base(outputPath)}, nil
	}

	finalPath := outputPath
	if len(source.AudioURLs) > 0 {
		audioKey := strings.Join(source.AudioURLs, "\x00")
		audioPath := audioCache[audioKey]
		if audioPath == "" {
			audioPath = filepath.Join(tempRoot, fmt.Sprintf("audio-%02d.m4a", index+1))
			if err := downloadResolverFile(ctx, source.AudioURLs, source.Headers, audioPath, resolverAudioMaxBytes); err != nil {
				return preparedResolverMedia{}, err
			}
			audioCache[audioKey] = audioPath
		}
		mergedPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + "-merged.mp4"
		if err := mergeResolverMedia(ctx, outputPath, audioPath, mergedPath, source.MergeAudio); err != nil {
			return preparedResolverMedia{}, err
		}
		finalPath = mergedPath
	}
	if settings.CompatibilityTranscode {
		transcodedPath := strings.TrimSuffix(finalPath, filepath.Ext(finalPath)) + "-compatible.mp4"
		if err := transcodeResolverVideo(ctx, finalPath, transcodedPath); err != nil {
			return preparedResolverMedia{}, err
		}
		finalPath = transcodedPath
	}
	return preparedResolverMedia{Kind: "video", Path: finalPath, Name: filepath.Base(finalPath)}, nil
}

func downloadResolverFile(ctx context.Context, candidates []string, headers map[string]string, destination string, maximum int64) error {
	client := &http.Client{}
	var lastErr error
	for _, candidate := range candidates {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(candidate), nil)
		if err != nil {
			lastErr = err
			continue
		}
		request.Header.Set("User-Agent", "Mozilla/5.0 RayleaBot Media Resolver")
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, err := client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = fmt.Errorf("媒体下载 HTTP %d", response.StatusCode)
			_ = response.Body.Close()
			continue
		}
		if response.ContentLength > maximum {
			lastErr = fmt.Errorf("媒体文件超过处理上限")
			_ = response.Body.Close()
			continue
		}
		file, createErr := os.Create(destination)
		if createErr != nil {
			_ = response.Body.Close()
			return createErr
		}
		written, copyErr := io.Copy(file, io.LimitReader(response.Body, maximum+1))
		closeErr := file.Close()
		_ = response.Body.Close()
		if copyErr != nil {
			lastErr = copyErr
			continue
		}
		if closeErr != nil {
			return closeErr
		}
		if written > maximum {
			lastErr = fmt.Errorf("媒体文件超过处理上限")
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的媒体地址")
	}
	return lastErr
}

func mergeResolverMedia(ctx context.Context, videoPath, audioPath, outputPath string, mixExisting bool) error {
	ffmpeg, err := resolverExecutable("RAYLEABOT_FFMPEG_PATH", "ffmpeg")
	if err != nil {
		return err
	}
	args := []string{"-y", "-i", videoPath, "-i", audioPath}
	if mixExisting && resolverMediaHasAudio(ctx, videoPath) {
		args = append(args, "-filter_complex", "[0:a][1:a]amix=inputs=2:duration=first:dropout_transition=2[a]", "-map", "0:v:0", "-map", "[a]", "-c:v", "copy", "-c:a", "aac", "-shortest", outputPath)
	} else {
		args = append(args, "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-c:a", "aac", "-shortest", outputPath)
	}
	return runResolverCommand(ctx, ffmpeg, args...)
}

func resolverMediaHasAudio(ctx context.Context, mediaPath string) bool {
	ffprobe, err := resolverExecutable("RAYLEABOT_FFPROBE_PATH", "ffprobe")
	if err != nil {
		return false
	}
	command := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "a", "-show_entries", "stream=index", "-of", "csv=p=0", mediaPath)
	output, err := command.Output()
	return err == nil && strings.TrimSpace(string(output)) != ""
}

func recordResolverLive(ctx context.Context, streamURL string, headers map[string]string, seconds int, outputPath string, transcode bool) error {
	ffmpeg, err := resolverExecutable("RAYLEABOT_FFMPEG_PATH", "ffmpeg")
	if err != nil {
		return err
	}
	args := []string{"-y"}
	if len(headers) > 0 {
		lines := make([]string, 0, len(headers))
		for key, value := range headers {
			lines = append(lines, key+": "+value)
		}
		args = append(args, "-headers", strings.Join(lines, "\r\n")+"\r\n")
	}
	args = append(args, "-i", streamURL, "-t", fmt.Sprint(seconds))
	if transcode {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "25", "-c:a", "aac")
	} else {
		args = append(args, "-c", "copy")
	}
	args = append(args, "-movflags", "+faststart", outputPath)
	return runResolverCommand(ctx, ffmpeg, args...)
}

func transcodeResolverVideo(ctx context.Context, inputPath, outputPath string) error {
	ffmpeg, err := resolverExecutable("RAYLEABOT_FFMPEG_PATH", "ffmpeg")
	if err != nil {
		return err
	}
	return runResolverCommand(ctx, ffmpeg, "-y", "-i", inputPath, "-c:v", "libx264", "-preset", "veryfast", "-crf", "25", "-c:a", "aac", "-movflags", "+faststart", outputPath)
}

func resolverExecutable(environment, fallback string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(environment)); value != "" {
		return value, nil
	}
	path, err := exec.LookPath(fallback)
	if err != nil {
		return "", errors.New("FFmpeg 运行环境不可用")
	}
	return path, nil
}

func runResolverCommand(ctx context.Context, executable string, args ...string) error {
	command := exec.CommandContext(ctx, executable, args...)
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := DiagnosticExcerpt(string(output), 300)
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("FFmpeg 处理失败：%s", detail)
}

func (handler *Handler) sendResolverVideo(ctx context.Context, event *rayleabot.EventContext, media preparedResolverMedia, settings ResolverMediaSettings) error {
	info, err := os.Stat(media.Path)
	if err != nil {
		return err
	}
	if settings.UploadOversize && info.Size() > int64(settings.VideoSizeLimitMB)<<20 {
		caller, ok := handler.hostActions(event).(GenericLocalActionCaller)
		if !ok {
			return errors.New("文件上传动作不可用")
		}
		action := "file.group.upload"
		request := map[string]any{"group_id": event.Event.Target.ID, "file": media.Path, "name": media.Name}
		if NormalizedTargetType(event.Event.Target.Type) == "private" {
			action = "file.private.upload"
			request = map[string]any{"user_id": event.Event.Target.ID, "file": media.Path, "name": media.Name}
		}
		var result map[string]any
		return caller.Call(ctx, action, request, &result)
	}
	_, err = handler.hostActions(event).MessageSend(ctx, rayleabot.MessageSendRequest{
		TargetType: NormalizedTargetType(event.Event.Target.Type), TargetID: event.Event.Target.ID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Passthrough("video", map[string]any{"file": media.Path})}},
	})
	return err
}

func sendResolverForward(ctx context.Context, actions HostActions, event *rayleabot.EventContext, platform string, media []preparedResolverMedia, batchSize int) error {
	caller, ok := actions.(GenericLocalActionCaller)
	if !ok {
		return errors.New("合并转发动作不可用")
	}
	batchSize = boundedSetting(batchSize, 50, 1, 100)
	name := FirstText(event.Bot.Nickname, resolverPlatformLabel(platform), "订阅与解析")
	uin := FirstText(event.Bot.ID, event.Event.Actor.ID)
	for offset := 0; offset < len(media); offset += batchSize {
		end := offset + batchSize
		if end > len(media) {
			end = len(media)
		}
		nodes := make([]map[string]any, 0, end-offset)
		for _, item := range media[offset:end] {
			segmentType := item.Kind
			data := map[string]any{"file": item.Path}
			nodes = append(nodes, map[string]any{
				"type": "node",
				"data": map[string]any{"name": name, "uin": uin, "content": []map[string]any{{"type": segmentType, "data": data}}},
			})
		}
		request := map[string]any{
			"target_type": NormalizedTargetType(event.Event.Target.Type), "target_id": event.Event.Target.ID,
			"messages": nodes,
		}
		var result map[string]any
		if err := caller.Call(ctx, "message.forward.send", request, &result); err != nil {
			return err
		}
	}
	return nil
}

func safeResolverFileName(value, kind string, index int) string {
	name := filepath.Base(strings.TrimSpace(value))
	if name == "." || name == "" {
		extension := ".mp4"
		if kind == "image" {
			extension = ".jpg"
		}
		name = fmt.Sprintf("media-%02d%s", index+1, extension)
	}
	if filepath.Ext(name) == "" {
		name += resolverExtension(value, kind)
	}
	return name
}

func resolverExtension(raw, kind string) string {
	if parsed, err := url.Parse(raw); err == nil {
		if extension := filepath.Ext(parsed.Path); len(extension) >= 2 && len(extension) <= 6 {
			return extension
		}
	}
	if kind == "image" {
		return ".jpg"
	}
	return ".mp4"
}
