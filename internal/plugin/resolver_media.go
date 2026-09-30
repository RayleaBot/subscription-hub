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
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	resolverImageMaxBytes    = 64 << 20
	resolverAudioMaxBytes    = 128 << 20
	resolverVideoMaxBytes    = 2 << 30
	resolverMediaReadTimeout = 15 * time.Second
)

type preparedResolverMedia struct {
	Kind string
	Path string
	Name string
}

func (handler *Handler) deliverResolverMedia(ctx context.Context, actions HostActions, platform string, plan ResolverMediaPlan, settings ResolverMediaSettings, target resolverSendTarget) (sendErr error) {
	job := &deferredMediaJob{}
	dispatched := false
	if !handler.deferredMedia.push(job) {
		return errors.New("媒体资源保留配额已满，请稍后再试")
	}
	defer func() {
		if dispatched && mediaOutcomeUncertain(sendErr) {
			job.retain()
		} else {
			handler.deferredMedia.remove(job)
			job.cleanup()
		}
	}()
	tempRoot, err := handler.deferredMedia.createTemp()
	if err != nil {
		return fmt.Errorf("创建临时目录：%w", err)
	}
	job.mu.Lock()
	job.tempRoot = tempRoot
	job.mu.Unlock()

	prepared, err := prepareResolverMediaSources(ctx, tempRoot, plan.Sources, settings)
	if err != nil {
		return err
	}
	if len(prepared) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dispatched = true
	return sendPreparedResolverMedia(ctx, actions, platform, prepared, settings, target)
}

type resolverSendTarget struct {
	TargetType    string
	TargetID      string
	SubjectName   string
	SenderID      string
	SenderName    string
	SourceAdapter string
}

func resolverMediaTarget(event *rayleabot.EventContext, platform string, update Update) resolverSendTarget {
	target := subscriptionMediaTarget(Subscription{
		Platform: platform, TargetType: event.Event.Target.Type, TargetID: event.Event.Target.ID,
		Subscribers: MergeSubscriber(nil, event),
	}, update)
	target.SourceAdapter = event.Event.SourceAdapter
	if target.SenderID == "" {
		target.SenderID = event.Bot.ID
		target.SenderName = FirstText(event.Bot.Nickname, "订阅与解析")
	}
	return target
}

func subscriptionMediaTarget(item Subscription, update Update) resolverSendTarget {
	target := resolverSendTarget{
		TargetType: NormalizedTargetType(item.TargetType), TargetID: item.TargetID,
		SubjectName: FirstText(NestedValue(update, "author", "name"), item.Name, item.UID, resolverPlatformLabel(item.Platform)),
		SenderName:  "订阅与解析",
	}
	for _, subscriber := range item.Subscribers {
		if id := strings.TrimSpace(subscriber.ID); id != "" {
			target.SenderID = id
			target.SenderName = FirstText(subscriber.GroupNickname, subscriber.Nickname, id)
			break
		}
	}
	// 旧订阅可能没有订阅人；群聊不借用触发检查的管理员身份。
	if target.SenderID == "" && target.TargetType == "private" {
		target.SenderID = target.TargetID
		target.SenderName = FirstText(item.TargetName, target.TargetID)
	}
	return target
}

func prepareResolverMediaSources(ctx context.Context, tempRoot string, sources []ResolverMediaSource, settings ResolverMediaSettings) ([]preparedResolverMedia, error) {
	prepared := make([]preparedResolverMedia, 0, len(sources))
	audioCache := map[string]string{}
	for index, source := range sources {
		item, err := prepareResolverMediaSource(ctx, tempRoot, index, source, settings, audioCache)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}
	return prepared, nil
}

func sendPreparedResolverMedia(ctx context.Context, actions HostActions, platform string, prepared []preparedResolverMedia, settings ResolverMediaSettings, target resolverSendTarget) error {
	return sendResolverForward(ctx, actions, platform, prepared, settings.ImageBatchSize, target)
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
	// 抖音视频 CDN 可能拒绝标准 HTTP 下载链，改用插件已有的托管
	// FFmpeg 媒体链；其他 CDN 继续使用 Go HTTP 客户端。
	if douyinVideoDownloadHosts(candidates) {
		return downloadResolverFileWithFFmpeg(ctx, candidates, headers, destination, maximum)
	}
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

// douyinVideoDownloadHosts 判断候选 URL 是否指向抖音视频 CDN。
func douyinVideoDownloadHosts(candidates []string) bool {
	for _, candidate := range candidates {
		parsed, err := url.Parse(strings.TrimSpace(candidate))
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if host == "aweme.snssdk.com" || strings.HasSuffix(host, ".douyinvod.com") || strings.HasSuffix(host, ".zjcdn.com") {
			return true
		}
	}
	return false
}

// downloadResolverFileWithFFmpeg 用托管 FFmpeg 跟随媒体重定向并封装输出。
func downloadResolverFileWithFFmpeg(ctx context.Context, candidates []string, headers map[string]string, destination string, maximum int64) error {
	ffmpeg, err := resolverExecutable("RAYLEABOT_FFMPEG_PATH", "ffmpeg")
	if err != nil {
		return err
	}
	headerLines := make([]string, 0, len(headers))
	for key, value := range headers {
		headerLines = append(headerLines, key+": "+value)
	}
	var lastErr error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = os.Remove(destination)
		// 限制网络连续无响应的时间，让停滞的 CDN 可以回退到备用地址；
		// 持续传输的大文件仍使用整个后台下载预算。
		args := []string{"-y", "-hide_banner", "-loglevel", "error", "-rw_timeout", fmt.Sprint(resolverMediaReadTimeout.Microseconds())}
		if len(headerLines) > 0 {
			args = append(args, "-headers", strings.Join(headerLines, "\r\n")+"\r\n")
		}
		args = append(args, "-i", strings.TrimSpace(candidate), "-c", "copy", "-movflags", "+faststart", destination)
		output, runErr := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput()
		if runErr == nil {
			info, statErr := os.Stat(destination)
			if statErr != nil {
				lastErr = statErr
				continue
			}
			if info.Size() > maximum {
				_ = os.Remove(destination)
				return fmt.Errorf("媒体文件超过处理上限")
			}
			return nil
		}
		detail := DiagnosticExcerpt(string(output), 300)
		if detail == "" {
			detail = runErr.Error()
		}
		lastErr = fmt.Errorf("FFmpeg 下载失败：%s", detail)
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

func sendResolverForward(ctx context.Context, actions HostActions, platform string, media []preparedResolverMedia, batchSize int, target resolverSendTarget) error {
	if len(media) == 0 {
		return nil
	}
	caller, ok := actions.(GenericLocalActionCaller)
	if !ok {
		return errors.New("合并转发动作不可用")
	}
	batchSize = boundedSetting(batchSize, 50, 1, 100)
	name := FirstText(target.SenderName, target.SenderID, "订阅与解析")
	for offset := 0; offset < len(media); offset += batchSize {
		end := offset + batchSize
		if end > len(media) {
			end = len(media)
		}
		nodes := make([]map[string]any, 0, end-offset)
		for _, item := range media[offset:end] {
			data := map[string]any{
				"name":    name,
				"content": []map[string]any{{"type": item.Kind, "data": map[string]any{"file": item.Path}}},
			}
			if target.SenderID != "" {
				data["uin"] = target.SenderID
			}
			nodes = append(nodes, map[string]any{
				"type": "node",
				"data": data,
			})
		}
		request := map[string]any{
			"target_type": target.TargetType, "target_id": target.TargetID,
			"messages": nodes,
			"source":   FirstText(target.SubjectName, resolverPlatformLabel(platform), "订阅与解析"),
		}
		if target.SourceAdapter != "" {
			request["source_adapter"] = target.SourceAdapter
		}
		var result map[string]any
		if err := caller.Call(ctx, "message.forward.send", request, &result); err != nil {
			if offset > 0 {
				return &partialMediaSendError{err}
			}
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
