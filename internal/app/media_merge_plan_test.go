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
	if plan.videoCount != 0 || plan.audioCount != 0 {
		t.Fatalf("编码一致时不应转码: video=%d audio=%d", plan.videoCount, plan.audioCount)
	}
	if _, err := mergeAudioEncoder(plan.audio); err != nil {
		t.Fatalf("LC 直接复制不应报错: %v", err)
	}
	if description := plan.description(); !strings.Contains(description, "不转码") {
		t.Fatalf("进度说明未标注不转码: %s", description)
	}
}

func TestMergePlanRefusesUnsupportedAudioTarget(t *testing.T) {
	hevc := mergePlanFixture("aac", "HE-AACv2", "hevc", "Main", 1920, 1080)
	lc := mergePlanFixture("aac", "LC", "h264", "High", 1280, 720)
	plan := planMergeMedia([]mergeMediaInfo{lc, hevc, hevc})

	if plan.audioCount != 1 || plan.videoCount != 1 {
		t.Fatalf("计划应只转换少数分集: audio=%d video=%d", plan.audioCount, plan.videoCount)
	}
	if _, err := mergeAudioEncoder(plan.audio); err == nil {
		t.Fatal("多数音轨为 HE-AACv2 时应直接报错，而不是改目标编码")
	} else if !strings.Contains(err.Error(), "HE-AACv2") {
		t.Fatalf("报错未指明不支持的档位: %v", err)
	}
}

func TestMergePlanAllowsSupportedCodecTargets(t *testing.T) {
	for _, codec := range []string{"aac", "mp3", "ac3", "eac3", "alac"} {
		profile := ""
		if codec == "aac" {
			profile = "LC"
		}
		info := mergePlanFixture(codec, profile, "hevc", "Main", 1920, 1080)
		if _, err := mergeAudioEncoder(info); err != nil {
			t.Fatalf("%s 应可直接作为目标编码: %v", codec, err)
		}
	}
}
