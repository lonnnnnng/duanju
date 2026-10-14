package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func mergeThreadCountFor(procs int) int {
	// long: 合并任务全局串行，按当前进程可用 CPU 提高单任务吞吐，同时限制上限避免低内存设备被 FFmpeg 抢占过多资源。
	if procs < 2 {
		return 2
	}
	if procs > 8 {
		return 8
	}
	return procs
}

func mergeThreadCount() int {
	return mergeThreadCountFor(runtime.GOMAXPROCS(0))
}

func runMergeFFmpeg(ctx context.Context, ffmpeg string, args []string, progress func(time.Duration)) error {
	command := exec.CommandContext(ctx, ffmpeg, append([]string{
		"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-progress", "pipe:1",
	}, args...)...)
	diagnostics := &cappedStringWriter{limit: 64 * 1024}
	command.Stderr = diagnostics
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	if err := command.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok && key == "out_time_us" && progress != nil {
			if micros, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && micros >= 0 {
				progress(time.Duration(micros) * time.Microsecond)
			}
		}
	}
	err = command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("FFmpeg 处理失败：%w %s", err, truncate(diagnostics.String(), 1500))
	}
	return scanner.Err()
}

func decodeMergedMedia(ctx context.Context, ffmpeg, path string, progress func(time.Duration)) error {
	// long: 合并完成和稍后清理分集使用同一套完整解码校验，避免两条路径对全集可用性的判断不一致。
	threads := strconv.Itoa(mergeThreadCount())
	return runMergeFFmpeg(ctx, ffmpeg, []string{"-xerror", "-err_detect", "explode", "-threads", threads, "-protocol_whitelist", "file,pipe", "-i", path,
		"-map", "0:v:0", "-map", "0:a:0?", "-threads:v", threads, "-fps_mode", "passthrough", "-abort_on", "empty_output", "-f", "null", "-"}, progress)
}

func mergeMediaFiles(ctx context.Context, ffmpeg string, paths []string, output string, report func(int, string)) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("没有可合并的分集")
	}
	lastProgress, lastDetail := -1, ""
	notify := func(progress int, detail string) {
		if progress > 99 {
			progress = 99
		}
		if progress < lastProgress {
			progress = lastProgress
		}
		if report != nil && (progress != lastProgress || detail != lastDetail) {
			report(progress, detail)
		}
		lastProgress, lastDetail = progress, detail
	}
	metadata := make([]mergeMediaInfo, 0, len(paths))
	var duration time.Duration
	for index, path := range paths {
		notify(index*5/len(paths), fmt.Sprintf("检测分集编码 %d/%d", index+1, len(paths)))
		info, err := probeMergeMedia(ctx, ffmpeg, path)
		if err != nil {
			return "", err
		}
		metadata = append(metadata, info)
		duration += info.duration
	}
	plan := planMergeMedia(metadata)
	method := plan.description()
	work, err := os.MkdirTemp(filepath.Dir(output), ".merge-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	inputs := append([]string(nil), paths...)
	var preparedDuration, processed time.Duration
	for index, step := range plan.steps {
		if step.video || step.audio || step.remux {
			preparedDuration += metadata[index].duration
		}
	}
	for index, step := range plan.steps {
		if !step.video && !step.audio && !step.remux {
			continue
		}
		action := "无损换封装"
		if step.video {
			action = "转换此集视频"
		} else if step.audio {
			action = "仅调整此集音轨"
		}
		detail := fmt.Sprintf("%s · %s %d/%d", method, action, index+1, len(paths))
		notify(5+int(processed*60/preparedDuration), detail)
		extension := ".mp4"
		if plan.transport {
			extension = ".ts"
		}
		temporary := filepath.Join(work, fmt.Sprintf("%06d%s", index+1, extension))
		args, err := plan.prepareArgs(paths[index], temporary, metadata[index], step)
		if err == nil {
			err = runMergeFFmpeg(ctx, ffmpeg, args, func(elapsed time.Duration) {
				if elapsed > metadata[index].duration {
					elapsed = metadata[index].duration
				}
				notify(5+int((processed+elapsed)*60/preparedDuration), detail)
			})
		}
		if err != nil {
			return "", fmt.Errorf("处理 %s 失败：%w；原分集已保留，未将整剧转码", filepath.Base(paths[index]), err)
		}
		processed += metadata[index].duration
		inputs[index] = temporary
	}
	var manifest strings.Builder
	manifest.WriteString("ffconcat version 1.0\n")
	for index, path := range inputs {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		if strings.ContainsAny(absolute, "\r\n") {
			return "", fmt.Errorf("分集路径包含换行符，无法合并")
		}
		fmt.Fprintf(&manifest, "file '%s'\n", strings.ReplaceAll(filepath.ToSlash(absolute), "'", "'\\''"))
		if plan.transport {
			fmt.Fprintf(&manifest, "duration %.6f\n", metadata[index].duration.Seconds())
		}
	}
	list := filepath.Join(work, "inputs.ffconcat")
	if err := os.WriteFile(list, []byte(manifest.String()), 0600); err != nil {
		return "", err
	}
	notify(65, method)
	args := []string{"-protocol_whitelist", "file,pipe", "-f", "concat", "-safe", "0", "-i", list,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn", "-c", "copy", "-video_track_timescale", strconv.Itoa(plan.timescale), "-movflags", "+faststart"}
	if plan.parameters {
		if plan.video.videoCodec == "h264" {
			args = append(args, "-tag:v", "avc3")
		} else if plan.video.videoCodec == "hevc" {
			args = append(args, "-tag:v", "hev1")
		}
	}
	args = append(args, "-y", output)
	err = runMergeFFmpeg(ctx, ffmpeg, args, func(elapsed time.Duration) {
		notify(65+int(elapsed*15/duration), method)
	})
	if err != nil {
		return "", err
	}
	notify(80, "核对合并文件时长")
	merged, err := probeMergeMedia(ctx, ffmpeg, output)
	if err != nil {
		return "", err
	}
	tolerance := duration / 100
	if tolerance < 2*time.Second {
		tolerance = 2 * time.Second
	}
	if merged.duration < duration-tolerance || merged.duration > duration+tolerance {
		return "", fmt.Errorf("合并文件时长不符（预计 %.1f 秒，实际 %.1f 秒），原分集已保留", duration.Seconds(), merged.duration.Seconds())
	}
	notify(80, "校验完整视频和音轨（只解码，不转码）")
	err = decodeMergedMedia(ctx, ffmpeg, output, func(elapsed time.Duration) {
		notify(80+int(elapsed*19/duration), "校验完整视频和音轨（只解码，不转码）")
	})
	if err != nil {
		return "", fmt.Errorf("合并文件解码校验失败：%w；原分集已保留", err)
	}
	return method, nil
}
