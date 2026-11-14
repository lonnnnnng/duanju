package app

import (
	"strings"
	"testing"
	"time"
)

func mergePlanFixture(codec, profile, videoCodec, videoProfile string, width, height int) mergeMediaInfo {
	return mergeMediaInfo{
		width: width, height: height, audio: true, duration: time.Minute,
		videoCodec: videoCodec, videoProfile: videoProfile, pixelFormat: "yuv420p",
		videoTB: "1/90000", audioTB: "1/44100",
		audioCodec: codec, audioProfile: profile, sampleRate: 44100, layout: "stereo",
	}
}

func TestMergePlanCopiesMatchingStreams(t *testing.T) {
	base := mergePlanFixture("aac", "LC", "hevc", "Main", 1920, 1080)
	plan := planMergeMedia([]mergeMediaInfo{base, base, base})
	if plan.videoCount != 0 || plan.audioCount != 0 || plan.audioNormalized {
		t.Fatalf("编码一致时不应转码: video=%d audio=%d normalized=%v", plan.videoCount, plan.audioCount, plan.audioNormalized)
	}
	if _, err := mergeAudioEncoder(plan.audio, plan.audioBitrate); err != nil {
		t.Fatalf("LC 直接复制不应报错: %v", err)
	}
	if description := plan.description(); !strings.Contains(description, "不转码") {
		t.Fatalf("进度说明未标注不转码: %s", description)
	}
}

func TestMergePlanNormalizesWhenMajorityNotEncodable(t *testing.T) {
	hevc := mergePlanFixture("aac", "HE-AACv2", "hevc", "Main", 1920, 1080)
	lc := mergePlanFixture("aac", "LC", "hevc", "Main", 1920, 1080)
	plan := planMergeMedia([]mergeMediaInfo{lc, hevc, hevc})

	if !plan.audioNormalized {
		t.Fatal("多数音轨为 HE-AACv2 时应统一重编 AAC-LC")
	}
	if plan.audio.audioCodec != "aac" || plan.audio.audioProfile != "LC" || plan.audioBitrate != mergeNormalizedAudioBitrate {
		t.Fatalf("统一目标错误: %+v bitrate=%d", plan.audio, plan.audioBitrate)
	}
	if plan.audioCount != 3 {
		t.Fatalf("统一音轨时应重编全部分集: %d", plan.audioCount)
	}
	if len(plan.audioDiffEpisodes) != 1 || plan.audioDiffEpisodes[0] != 1 {
		t.Fatalf("应记录音轨不同的分集: %v", plan.audioDiffEpisodes)
	}
	description := plan.description()
	if !strings.Contains(description, "统一音轨 3/3 集（AAC-LC，因 001 集音轨不同）") {
		t.Fatalf("进度说明未说明统一原因: %s", description)
	}
	if _, err := mergeAudioEncoder(plan.audio, plan.audioBitrate); err != nil {
		t.Fatalf("AAC-LC 目标应可编码: %v", err)
	}
}

func TestMergePlanKeepsUniformHEAACWithoutReencoding(t *testing.T) {
	hevc := mergePlanFixture("aac", "HE-AACv2", "hevc", "Main", 1920, 1080)
	plan := planMergeMedia([]mergeMediaInfo{hevc, hevc, hevc})
	if plan.audioNormalized || plan.audioCount != 0 {
		t.Fatalf("全部同档位时不应重编: normalized=%v audio=%d", plan.audioNormalized, plan.audioCount)
	}
}

func TestMergePlanAllowsSupportedCodecTargets(t *testing.T) {
	for _, codec := range []string{"aac", "mp3", "ac3", "eac3", "alac"} {
		profile := ""
		if codec == "aac" {
			profile = "LC"
		}
		info := mergePlanFixture(codec, profile, "hevc", "Main", 1920, 1080)
		if _, err := mergeAudioEncoder(info, 0); err != nil {
			t.Fatalf("%s 应可直接作为目标编码: %v", codec, err)
		}
	}
}

func TestFormatMergeEpisodesShortensLongLists(t *testing.T) {
	if text := formatMergeEpisodes([]int{1, 49}); text != "001、049" {
		t.Fatalf("短列表格式错误: %s", text)
	}
	if text := formatMergeEpisodes([]int{1, 2, 3, 4, 5}); text != "001、002、003 等 5 集" {
		t.Fatalf("长列表格式错误: %s", text)
	}
	if text := formatMergeEpisodes(nil); text != "分集" {
		t.Fatalf("空列表格式错误: %s", text)
	}
}
