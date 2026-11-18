package app

import (
	"bufio"
	"context"
	"duanju/internal/diskspace"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// mergeWorkerCount 决定同时运行多少个 FFmpeg 子任务，避免把 CPU 全部抢光。
func mergeWorkerCount(jobs int) int {
	if jobs <= 1 {
		return 1
	}
	workers := mergeThreadCount() / 2
	if workers < 1 {
		workers = 1
	}
	if workers > 4 {
		workers = 4
	}
	if workers > jobs {
		workers = jobs
	}
	return workers
}

// mergeJobThreads 把总线程预算平均分给并发任务，至少 1 线程。
func mergeJobThreads(workers int) int {
	if workers < 1 {
		return mergeThreadCount()
	}
	threads := mergeThreadCount() / workers
	if threads < 1 {
		return 1
	}
	return threads
}

type mergeTimeRange struct {
	start  time.Duration
	length time.Duration
}

// mergeVerifyRanges 把整片时长切成若干段，用于并行解码校验。
func mergeVerifyRanges(duration time.Duration, workers int) []mergeTimeRange {
	if workers < 1 {
		workers = 1
	}
	chunk := duration / time.Duration(workers)
	if chunk <= 0 {
		return []mergeTimeRange{{start: 0, length: duration}}
	}
	ranges := make([]mergeTimeRange, 0, workers)
	for index := 0; index < workers; index++ {
		start := chunk * time.Duration(index)
		length := chunk
		if index == workers-1 {
			length = duration - start
		}
		if length <= 0 {
			break
		}
		ranges = append(ranges, mergeTimeRange{start: start, length: length})
	}
	return ranges
}

// mergeVerifyWorkers 按核数和片长决定并行段数，短片不强行切分。
func mergeVerifyWorkers(duration time.Duration) int {
	workers := mergeThreadCount() / 2
	if workers < 2 {
		workers = 2
	}
	if workers > 6 {
		workers = 6
	}
	if limit := int(duration / (20 * time.Second)); limit < workers {
		workers = limit
	}
	if workers < 1 {
		workers = 1
	}
	return workers
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
		if spaceErr := diskspace.FFmpegError(diagnostics.String()); spaceErr != nil {
			return spaceErr
		}
		return fmt.Errorf("FFmpeg 处理失败：%w %s", err, truncate(diagnostics.String(), 1500))
	}
	return scanner.Err()
}

const (
	mergeVerifySampleThreshold = 10 * time.Minute
	mergeVerifySampleWindow    = 20 * time.Second
	mergeVerifySampleCount     = 8
	mergeVerifySampleMaxRatio  = 35
)

func mergeVerifyFullRequested() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("DUANJU_MERGE_VERIFY")), "full")
}

// mergeVerifySampleRanges 为长片生成“首尾 + 均匀片段 + 转码分集”的抽样范围。
// 抽样总量超过 35% 时退回完整解码，避免抽样反而不省时间。
func mergeVerifySampleRanges(duration time.Duration, transcoded []mergeTimeRange) ([]mergeTimeRange, time.Duration, bool) {
	if duration <= 0 {
		return nil, 0, false
	}
	if mergeVerifyFullRequested() || duration <= mergeVerifySampleThreshold {
		return []mergeTimeRange{{start: 0, length: duration}}, duration, true
	}
	window := mergeVerifySampleWindow
	if window > duration {
		window = duration
	}
	ranges := []mergeTimeRange{
		{start: 0, length: window},
		{start: duration - window, length: window},
	}
	for index := 1; index <= mergeVerifySampleCount; index++ {
		start := duration * time.Duration(index) / time.Duration(mergeVerifySampleCount+1)
		if start+window > duration {
			start = duration - window
		}
		ranges = append(ranges, mergeTimeRange{start: start, length: window})
	}
	for _, span := range transcoded {
		start := span.start - 2*time.Second
		if start < 0 {
			start = 0
		}
		end := span.start + span.length + 2*time.Second
		if end > duration {
			end = duration
		}
		if end > start {
			ranges = append(ranges, mergeTimeRange{start: start, length: end - start})
		}
	}
	merged := mergeNormalizeRanges(ranges, duration)
	var total time.Duration
	for _, span := range merged {
		total += span.length
	}
	if total <= 0 || total*100 > duration*mergeVerifySampleMaxRatio {
		return []mergeTimeRange{{start: 0, length: duration}}, duration, true
	}
	return merged, total, false
}

func mergeNormalizeRanges(ranges []mergeTimeRange, duration time.Duration) []mergeTimeRange {
	clamped := make([]mergeTimeRange, 0, len(ranges))
	for _, span := range ranges {
		start := span.start
		if start < 0 {
			start = 0
		}
		if start >= duration {
			continue
		}
		length := span.length
		if start+length > duration {
			length = duration - start
		}
		if length <= 0 {
			continue
		}
		clamped = append(clamped, mergeTimeRange{start: start, length: length})
	}
	sort.Slice(clamped, func(i, j int) bool { return clamped[i].start < clamped[j].start })
	merged := make([]mergeTimeRange, 0, len(clamped))
	for _, span := range clamped {
		if len(merged) == 0 {
			merged = append(merged, span)
			continue
		}
		last := &merged[len(merged)-1]
		if span.start <= last.start+last.length+time.Second {
			end := span.start + span.length
			if end > last.start+last.length {
				last.length = end - last.start
			}
			continue
		}
		merged = append(merged, span)
	}
	return merged
}

// verifyMergedMedia 并行分段解码整片：保留“完整解码”的语义，同时把耗时压到 1/N。
func verifyMergedMedia(ctx context.Context, ffmpeg, path string, duration time.Duration, progress func(time.Duration)) error {
	if duration <= 0 {
		return decodeMergedMedia(ctx, ffmpeg, path, progress)
	}
	workers := mergeVerifyWorkers(duration)
	return verifyMergeSpans(ctx, ffmpeg, path, mergeVerifyRanges(duration, workers), duration, progress)
}

// verifyMergedMediaRanges 只解码抽样片段，避免长片每次都完整解码。
func verifyMergedMediaRanges(ctx context.Context, ffmpeg, path string, spans []mergeTimeRange, total time.Duration, progress func(time.Duration)) error {
	if total <= 0 || len(spans) == 0 {
		return decodeMergedMedia(ctx, ffmpeg, path, progress)
	}
	return verifyMergeSpans(ctx, ffmpeg, path, spans, total, progress)
}

func verifyMergeSpans(ctx context.Context, ffmpeg, path string, spans []mergeTimeRange, total time.Duration, progress func(time.Duration)) error {
	workers := mergeVerifyWorkers(total)
	if workers > len(spans) {
		workers = len(spans)
	}
	if workers < 1 {
		workers = 1
	}
	threads := mergeJobThreads(workers)
	verifyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	elapsed := make([]time.Duration, len(spans))
	sem := make(chan struct{}, workers)
	var firstErr error
	for index, span := range spans {
		wg.Add(1)
		go func(index int, span mergeTimeRange) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-verifyCtx.Done():
				return
			}
			args := []string{"-xerror", "-err_detect", "explode", "-threads", strconv.Itoa(threads), "-protocol_whitelist", "file,pipe",
				"-ss", strconv.FormatFloat(span.start.Seconds(), 'f', 6, 64),
				"-t", strconv.FormatFloat(span.length.Seconds(), 'f', 6, 64),
				"-i", path, "-map", "0:v:0", "-map", "0:a:0?", "-threads:v", strconv.Itoa(threads),
				"-fps_mode", "passthrough", "-abort_on", "empty_output", "-f", "null", "-"}
			err := runMergeFFmpeg(verifyCtx, ffmpeg, args, func(value time.Duration) {
				if value > span.length {
					value = span.length
				}
				mu.Lock()
				elapsed[index] = value
				var sum time.Duration
				for _, item := range elapsed {
					sum += item
				}
				mu.Unlock()
				if progress != nil {
					progress(sum)
				}
			})
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("合并文件 %.0f–%.0f 秒解码校验失败：%w", span.start.Seconds(), (span.start + span.length).Seconds(), err)
				}
				mu.Unlock()
				cancel()
			}
		}(index, span)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return firstErr
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
	var notifyMu sync.Mutex
	notify := func(progress int, detail string) {
		notifyMu.Lock()
		defer notifyMu.Unlock()
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
	metadata := make([]mergeMediaInfo, len(paths))
	var duration time.Duration
	probeCtx, probeCancel := context.WithCancel(ctx)
	var probeMu sync.Mutex
	var probeErr error
	probeDone := 0
	probeSem := make(chan struct{}, mergeWorkerCount(len(paths)))
	var probeWG sync.WaitGroup
	for index := range paths {
		probeWG.Add(1)
		go func(index int) {
			defer probeWG.Done()
			probeSem <- struct{}{}
			defer func() { <-probeSem }()
			info, err := probeMergeMedia(probeCtx, ffmpeg, paths[index])
			probeMu.Lock()
			defer probeMu.Unlock()
			if err != nil {
				if probeErr == nil {
					probeErr = err
					probeCancel()
				}
				return
			}
			metadata[index] = info
			duration += info.duration
			probeDone++
			notify(probeDone*5/len(paths), fmt.Sprintf("检测分集编码 %d/%d", probeDone, len(paths)))
		}(index)
	}
	probeWG.Wait()
	probeCancel()
	if probeErr != nil {
		return "", probeErr
	}
	plan := planMergeMedia(metadata)
	method := plan.description()
	work, err := os.MkdirTemp(filepath.Dir(output), ".merge-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	inputs := append([]string(nil), paths...)
	var preparedDuration time.Duration
	var jobs []int
	for index, step := range plan.steps {
		if step.video || step.audio || step.remux {
			jobs = append(jobs, index)
			preparedDuration += metadata[index].duration
		}
	}
	workers := mergeWorkerCount(len(jobs))
	threads := mergeJobThreads(workers)
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()
	jobSem := make(chan struct{}, workers)
	var jobWG sync.WaitGroup
	var jobMu sync.Mutex
	elapsedByJob := make([]time.Duration, len(paths))
	finished := 0
	var firstErr error
	for _, index := range jobs {
		index := index
		step := plan.steps[index]
		action := "无损换封装"
		if step.video {
			action = "转换此集视频"
		} else if step.audio {
			action = "仅调整此集音轨"
		}
		jobWG.Add(1)
		go func() {
			defer jobWG.Done()
			jobSem <- struct{}{}
			defer func() { <-jobSem }()
			extension := ".mp4"
			if plan.transport {
				extension = ".ts"
			}
			temporary := filepath.Join(work, fmt.Sprintf("%06d%s", index+1, extension))
			args, err := plan.prepareArgs(paths[index], temporary, metadata[index], step, threads)
			if err == nil {
				err = runMergeFFmpeg(jobCtx, ffmpeg, args, func(elapsed time.Duration) {
					if elapsed > metadata[index].duration {
						elapsed = metadata[index].duration
					}
					jobMu.Lock()
					elapsedByJob[index] = elapsed
					var total time.Duration
					for _, value := range elapsedByJob {
						total += value
					}
					jobMu.Unlock()
					if preparedDuration > 0 {
						notify(5+int(total*60/preparedDuration), method+" · "+action)
					}
				})
			}
			jobMu.Lock()
			defer jobMu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("处理 %s 失败：%w；原分集已保留，未将整剧转码；如该剧分集编码不一致，请重新下载后再合并", filepath.Base(paths[index]), err)
					jobCancel()
				}
				return
			}
			elapsedByJob[index] = metadata[index].duration
			inputs[index] = temporary
			finished++
			notify(5+finished*60/len(jobs), fmt.Sprintf("%s · %s %d/%d", method, action, finished, len(jobs)))
		}()
	}
	jobWG.Wait()
	if firstErr != nil {
		return "", firstErr
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
	transcoded := make([]mergeTimeRange, 0, len(metadata))
	var offset time.Duration
	for index, info := range metadata {
		step := plan.steps[index]
		if step.video || step.audio {
			transcoded = append(transcoded, mergeTimeRange{start: offset, length: info.duration})
		}
		offset += info.duration
	}
	spans, verifyTotal, fullVerify := mergeVerifySampleRanges(merged.duration, transcoded)
	detail := "校验完整视频和音轨（只解码，不转码）"
	if !fullVerify {
		detail = "抽样校验关键片段（只解码，不转码）"
	}
	notify(80, detail)
	if fullVerify {
		err = verifyMergedMedia(ctx, ffmpeg, output, merged.duration, func(elapsed time.Duration) {
			notify(80+int(elapsed*19/merged.duration), detail)
		})
	} else {
		err = verifyMergedMediaRanges(ctx, ffmpeg, output, spans, verifyTotal, func(elapsed time.Duration) {
			notify(80+int(elapsed*19/verifyTotal), detail)
		})
	}
	if err != nil {
		return "", fmt.Errorf("合并文件解码校验失败：%w；原分集已保留", err)
	}
	return method, nil
}
