package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type mergeMediaPlan struct {
	video, audio mergeMediaInfo
	steps        []mergeMediaStep
	timescale    int
	transport    bool
	parameters   bool
	videoCount   int
	audioCount   int
	remuxCount   int
	// audioNormalized 表示音轨档位不一致且多数档位无法编码，已统一重编为 AAC-LC。
	audioNormalized bool
	// audioBitrate 是重编音轨的目标码率；0 表示沿用默认值。
	audioBitrate int
	// audioDiffEpisodes 记录音轨与多数不一致的分集序号（1 起），用于进度说明。
	audioDiffEpisodes []int
}

type mergeMediaStep struct {
	video, audio, remux bool
}

func (info mergeMediaInfo) videoKey() string {
	key := fmt.Sprintf("%s|%s|%s|%dx%d|%s", info.videoCodec, info.videoProfile, info.pixelFormat, info.width, info.height, info.sar)
	if info.videoCodec != "h264" && info.videoCodec != "hevc" {
		key += "|" + info.videoExtra
	}
	return key
}

func (info mergeMediaInfo) audioKey() string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", info.audioCodec, info.audioProfile, info.sampleRate, info.layout, info.audioExtra)
}

// mergeVideoDimensionTolerance 允许 HEVC/H.264 分集存在极小的尺寸差异（例如 1080×1924 与 1080×1920）。
// 这类差异通过带内参数集和 MPEG-TS 中转保留，避免为 4px 差异整集重编码。
const mergeVideoDimensionTolerance = 8

func mergeVideoCompatible(info, target mergeMediaInfo) bool {
	if info.videoCodec != target.videoCodec || info.videoProfile != target.videoProfile ||
		info.pixelFormat != target.pixelFormat || info.sar != target.sar {
		return false
	}
	if info.videoCodec == "h264" || info.videoCodec == "hevc" {
		return mergeIntAbs(info.width-target.width) <= mergeVideoDimensionTolerance &&
			mergeIntAbs(info.height-target.height) <= mergeVideoDimensionTolerance
	}
	return info.videoKey() == target.videoKey()
}

func mergeIntAbs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func majorityMergeMedia(infos []mergeMediaInfo, key func(mergeMediaInfo) string, audio bool) mergeMediaInfo {
	counts := make(map[string]int)
	durations := make(map[string]time.Duration)
	for _, info := range infos {
		if audio && !info.audio {
			continue
		}
		value := key(info)
		counts[value]++
		durations[value] += info.duration
	}
	var chosen mergeMediaInfo
	bestCount, bestDuration := 0, time.Duration(0)
	for _, info := range infos {
		if audio && !info.audio {
			continue
		}
		value := key(info)
		if counts[value] > bestCount || counts[value] == bestCount && durations[value] > bestDuration {
			chosen, bestCount, bestDuration = info, counts[value], durations[value]
		}
	}
	return chosen
}

func planMergeMedia(infos []mergeMediaInfo) mergeMediaPlan {
	plan := mergeMediaPlan{timescale: 90000}
	plan.video = majorityMergeMedia(infos, mergeMediaInfo.videoKey, false)
	var compatible []mergeMediaInfo
	for _, info := range infos {
		if mergeVideoCompatible(info, plan.video) {
			compatible = append(compatible, info)
		}
	}
	plan.video = majorityMergeMedia(compatible, func(info mergeMediaInfo) string { return info.videoTB }, false)
	plan.audio = majorityMergeMedia(infos, mergeMediaInfo.audioKey, true)
	if value, ok := strings.CutPrefix(plan.video.videoTB, "1/"); ok {
		if scale, err := strconv.Atoi(value); err == nil && scale > 0 && scale <= 10000000 {
			plan.timescale = scale
		}
	}
	for _, info := range infos {
		step := mergeMediaStep{video: !mergeVideoCompatible(info, plan.video), audio: plan.audio.audio && (!info.audio || info.audioKey() != plan.audio.audioKey())}
		if step.video || info.videoExtra != plan.video.videoExtra || info.width != plan.video.width || info.height != plan.video.height {
			plan.parameters = true
		}
		step.remux = info.videoTB != fmt.Sprintf("1/%d", plan.timescale) || plan.audio.audio && info.audioTB != plan.audio.audioTB || info.reorder
		plan.steps = append(plan.steps, step)
	}
	plan.transport = (plan.video.videoCodec == "hevc" || plan.video.videoCodec == "h264") && plan.parameters
	for i := range plan.steps {
		step := &plan.steps[i]
		step.remux = step.remux || plan.transport
	}
	plan.countSteps()
	// long: 上游可能个别分集封装了不同音轨档位（例如 1 集 AAC-LC、其余 HE-AACv2）。
	// FFmpeg 编不出 HE-AACv2，此时只能把所有音轨统一重编为 AAC-LC，否则整剧无法合并。
	if plan.audioCount > 0 && !mergeAudioTargetEncodable(plan.audio) {
		plan.audioDiffEpisodes = plan.audioDiffs(infos)
		plan.audio.audioCodec, plan.audio.audioProfile, plan.audio.audioExtra = "aac", "LC", ""
		plan.audioNormalized = true
		plan.audioBitrate = mergeNormalizedAudioBitrate
		for i := range plan.steps {
			plan.steps[i].audio = true
		}
		plan.countSteps()
	}
	return plan
}

func (plan *mergeMediaPlan) countSteps() {
	plan.videoCount, plan.audioCount, plan.remuxCount = 0, 0, 0
	for _, step := range plan.steps {
		if step.video {
			plan.videoCount++
		}
		if step.audio {
			plan.audioCount++
		}
		if step.remux && !step.video && !step.audio {
			plan.remuxCount++
		}
	}
}

// audioDiffs 返回音轨档位与多数不一致的分集序号（1 起）。
func (plan mergeMediaPlan) audioDiffs(infos []mergeMediaInfo) []int {
	diffs := make([]int, 0, 4)
	for index, info := range infos {
		if !plan.audio.audio || !info.audio || info.audioKey() != plan.audio.audioKey() {
			diffs = append(diffs, index+1)
		}
	}
	return diffs
}

// formatMergeEpisodes 把分集序号压成简短说明，最多列出 3 个。
func formatMergeEpisodes(episodes []int) string {
	if len(episodes) == 0 {
		return "分集"
	}
	const limit = 3
	parts := make([]string, 0, limit)
	for index, episode := range episodes {
		if index >= limit {
			break
		}
		parts = append(parts, fmt.Sprintf("%03d", episode))
	}
	text := strings.Join(parts, "、")
	if len(episodes) > limit {
		text += fmt.Sprintf(" 等 %d 集", len(episodes))
	}
	return text
}

// mergeAudioTargetEncodable 判断当前 FFmpeg 原生编码器能否产出多数音轨的编码档位。
func mergeAudioTargetEncodable(info mergeMediaInfo) bool {
	if !info.audio {
		return true
	}
	switch info.audioCodec {
	case "aac":
		return info.audioProfile == "" || info.audioProfile == "LC"
	case "mp3", "ac3", "eac3", "alac":
		return true
	default:
		return false
	}
}

func (plan mergeMediaPlan) description() string {
	parts := []string{}
	if plan.videoCount > 0 {
		parts = append(parts, fmt.Sprintf("仅转视频 %d/%d 集（%s %dx%d）", plan.videoCount, len(plan.steps), strings.ToUpper(plan.video.videoCodec), plan.video.width, plan.video.height))
	}
	if plan.audioCount > 0 {
		if plan.audioNormalized {
			parts = append(parts, fmt.Sprintf("统一音轨 %d/%d 集（AAC-LC，因 %s 集音轨不同）",
				plan.audioCount, len(plan.steps), formatMergeEpisodes(plan.audioDiffEpisodes)))
		} else {
			parts = append(parts, fmt.Sprintf("调整音轨 %d/%d 集", plan.audioCount, len(plan.steps)))
		}
	}
	if len(parts) == 0 {
		if plan.remuxCount > 0 {
			return "无损换封装合并（不转码）"
		}
		return "原编码快速合并（不转码）"
	}
	return strings.Join(parts, "；")
}

func mergeVideoEncoder(info mergeMediaInfo, threads int) ([]string, error) {
	profile := strings.ToLower(info.videoProfile)
	var args []string
	switch info.videoCodec {
	case "h264":
		profiles := map[string]string{"constrained baseline": "baseline", "baseline": "baseline", "main": "main", "high": "high", "high 10": "high10", "high 4:2:2": "high422", "high 4:4:4 predictive": "high444"}
		value, ok := profiles[profile]
		if !ok {
			return nil, fmt.Errorf("暂不能将少数分集转为多数使用的 H.264 %s", info.videoProfile)
		}
		args = []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-profile:v", value, "-threads:v", strconv.Itoa(threads)}
	case "hevc":
		profiles := map[string]string{"main": "main", "main 10": "main10"}
		value, ok := profiles[profile]
		if !ok {
			return nil, fmt.Errorf("暂不能将少数分集转为多数使用的 HEVC %s", info.videoProfile)
		}
		args = []string{"-c:v", "libx265", "-preset", "veryfast", "-crf", "18", "-profile:v", value, "-threads:v", strconv.Itoa(threads), "-x265-params", fmt.Sprintf("pools=%d:frame-threads=%d:log-level=error", threads, threads)}
	default:
		return nil, fmt.Errorf("暂不支持将少数分集转为多数使用的 %s 编码；未对整剧转码", info.videoCodec)
	}
	if info.pixelFormat == "" || info.width%2 != 0 || info.height%2 != 0 {
		return nil, fmt.Errorf("多数分集的像素格式或尺寸无法安全转换")
	}
	return append(args, "-pix_fmt", info.pixelFormat), nil
}

const mergeNormalizedAudioBitrate = 128000

func mergeAudioEncoder(info mergeMediaInfo, bitrate int) ([]string, error) {
	encoder := info.audioCodec
	switch encoder {
	case "aac":
		if info.audioProfile != "" && info.audioProfile != "LC" {
			return nil, fmt.Errorf("暂不能将少数音轨转为多数使用的 AAC %s", info.audioProfile)
		}
	case "mp3":
		encoder = "libmp3lame"
	case "ac3", "eac3", "alac":
	default:
		return nil, fmt.Errorf("暂不支持将少数音轨转为多数使用的 %s", info.audioCodec)
	}
	if info.sampleRate <= 0 || info.layout == "" {
		return nil, fmt.Errorf("多数音轨缺少采样率或声道布局，无法安全转换")
	}
	args := []string{"-c:a", encoder, "-ar", strconv.Itoa(info.sampleRate), "-channel_layout:a", info.layout}
	if encoder == "aac" {
		if bitrate <= 0 {
			bitrate = 192000
		}
		args = append(args, "-profile:a", "aac_low", "-b:a", strconv.Itoa(bitrate/1000)+"k")
	}
	return args, nil
}

func (plan mergeMediaPlan) prepareArgs(input, output string, info mergeMediaInfo, step mergeMediaStep, threads int) ([]string, error) {
	if threads < 1 {
		threads = 1
	}
	args := []string{"-xerror", "-threads", strconv.Itoa(threads), "-protocol_whitelist", "file,pipe", "-i", input}
	if plan.audio.audio && !info.audio {
		args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("anullsrc=channel_layout=%s:sample_rate=%d", plan.audio.layout, plan.audio.sampleRate))
	}
	args = append(args, "-map", "0:v:0", "-sn", "-dn", "-map_metadata", "-1", "-c:v", "copy")
	if step.video {
		encoder, err := mergeVideoEncoder(plan.video, threads)
		if err != nil {
			return nil, err
		}
		sar := plan.video.sar
		if sar == "" || sar == "0/1" {
			sar = "1/1"
		}
		filter := "setpts=PTS-STARTPTS,"
		if plan.video.frameRate > 0 {
			filter += "fps=" + strconv.FormatFloat(plan.video.frameRate, 'f', -1, 64) + ","
		}
		filter += fmt.Sprintf("scale=w='max(2,trunc(min(%d,%d*dar/(%s))/2)*2)':h='max(2,trunc(min(%d,%d*(%s)/dar)/2)*2)',pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=%s", plan.video.width, plan.video.height, sar, plan.video.height, plan.video.width, sar, plan.video.width, plan.video.height, sar)
		args = append(args, "-vf", filter)
		args = append(args, encoder...)
	}
	if plan.audio.audio {
		stream := "0:a:0"
		if !info.audio {
			stream = "1:a:0"
		}
		args = append(args, "-map", stream)
		if step.audio {
			encoder, err := mergeAudioEncoder(plan.audio, plan.audioBitrate)
			if err != nil {
				return nil, err
			}
			args = append(args, encoder...)
			args = append(args, "-af", fmt.Sprintf("aresample=%d:async=1:first_pts=0,apad", plan.audio.sampleRate))
		} else {
			args = append(args, "-c:a", "copy")
		}
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-max_muxing_queue_size", "4096")
	if step.video || step.audio {
		args = append(args, "-t", strconv.FormatFloat(info.duration.Seconds(), 'f', 6, 64))
	}
	if plan.transport {
		bitstream := "hevc_mp4toannexb"
		if plan.video.videoCodec == "h264" {
			bitstream = "h264_mp4toannexb"
		}
		args = append(args, "-bsf:v", bitstream, "-f", "mpegts")
	} else {
		args = append(args, "-video_track_timescale", strconv.Itoa(plan.timescale), "-f", "mp4")
	}
	return append(args, "-y", output), nil
}
