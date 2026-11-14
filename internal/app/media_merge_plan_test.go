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

func TestMergePlanNormalizesUnsupportedAudioProfile(t *testing.T) {
	hevc := mergePlanFixture("aac", "HE-AACv2", "hevc", "Main", 1920, 1080)
	h264 := mergePlanFixture("aac", "LC", "h264", "High", 1280, 720)
	plan := planMergeMedia([]mergeMediaInfo{h264, hevc, hevc, hevc})

	if !plan.audioNormalized || plan.audio.audioCodec != "aac" || plan.audio.audioProfile != "LC" {
		t.Fatalf("多数 HE-AACv2 应回退为 AAC-LC: %+v", plan.audio)
	}
	if plan.audioCount != 4 || plan.videoCount != 1 {
		t.Fatalf("回退后应重编全部音轨、只转少数视频: audio=%d video=%d", plan.audioCount, plan.videoCount)
	}
	if description := plan.description(); !strings.Contains(description, "统一音轨 4/4 集（AAC-LC）") {
		t.Fatalf("进度说明未标注统一音轨: %s", description)
	}
	if _, err := mergeAudioEncoder(plan.audio); err != nil {
		t.Fatalf("AAC-LC 目标应可编码: %v", err)
	}
}

func TestMergePlanKeepsLCCopyWhenProfilesMatch(t *testing.T) {
	base := mergePlanFixture("aac", "LC", "hevc", "Main", 1920, 1080)
	plan := planMergeMedia([]mergeMediaInfo{base, base, base})
	if plan.audioNormalized || plan.audioCount != 0 {
		t.Fatalf("音轨一致时不应重编: normalized=%v audio=%d", plan.audioNormalized, plan.audioCount)
	}
	if _, err := mergeAudioEncoder(plan.audio); err != nil {
		t.Fatalf("LC 直接复制不应报错: %v", err)
	}
}

func TestMergePlanNormalizesUnsupportedAudioCodec(t *testing.T) {
	opus := mergePlanFixture("opus", "", "hevc", "Main", 1920, 1080)
	aac := mergePlanFixture("aac", "LC", "hevc", "Main", 1920, 1080)
	// 多数是 opus，而 FFmpeg 没有可用的 opus 编码目标，少数 aac 无法转成 opus。
	plan := planMergeMedia([]mergeMediaInfo{opus, opus, aac})
	if !plan.audioNormalized || plan.audio.audioCodec != "aac" || plan.audio.audioProfile != "LC" {
		t.Fatalf("不支持的多数音轨应回退 AAC-LC: %+v", plan.audio)
	}
	if plan.audioCount != 3 {
		t.Fatalf("回退后应重编全部音轨: %d", plan.audioCount)
	}
}

func TestMergePlanCopiesUniformUnsupportedCodec(t *testing.T) {
	opus := mergePlanFixture("opus", "", "hevc", "Main", 1920, 1080)
	plan := planMergeMedia([]mergeMediaInfo{opus, opus, opus})
	if plan.audioNormalized || plan.audioCount != 0 {
		t.Fatalf("全部同编码时无需转码: normalized=%v audio=%d", plan.audioNormalized, plan.audioCount)
	}
}
