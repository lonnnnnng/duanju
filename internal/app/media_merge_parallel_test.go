package app

import (
	"testing"
	"time"
)

func TestMergeVerifyRangesCoversWholeDuration(t *testing.T) {
	duration := 100 * time.Second
	for _, workers := range []int{1, 2, 4, 6} {
		ranges := mergeVerifyRanges(duration, workers)
		if len(ranges) != workers {
			t.Fatalf("workers=%d 段数错误: %d", workers, len(ranges))
		}
		var total time.Duration
		var previousEnd time.Duration
		for index, span := range ranges {
			if span.length <= 0 {
				t.Fatalf("workers=%d 第 %d 段长度无效: %v", workers, index, span.length)
			}
			if span.start != previousEnd {
				t.Fatalf("workers=%d 第 %d 段起点不连续: %v != %v", workers, index, span.start, previousEnd)
			}
			previousEnd = span.start + span.length
			total += span.length
		}
		if total != duration || previousEnd != duration {
			t.Fatalf("workers=%d 分段未覆盖整片: total=%v end=%v", workers, total, previousEnd)
		}
	}
}

func TestMergeVerifyRangesHandlesShortMedia(t *testing.T) {
	ranges := mergeVerifyRanges(3*time.Second, 4)
	if len(ranges) == 0 {
		t.Fatal("短片也应至少有一段")
	}
	if ranges[0].start != 0 || ranges[len(ranges)-1].start+ranges[len(ranges)-1].length != 3*time.Second {
		t.Fatalf("短片分段错误: %+v", ranges)
	}
	for _, span := range ranges {
		if span.length <= 0 {
			t.Fatalf("分段长度为 0: %+v", ranges)
		}
	}
}

func TestMergeVerifyWorkersStayWithinBounds(t *testing.T) {
	if workers := mergeVerifyWorkers(2 * time.Second); workers != 1 {
		t.Fatalf("超短片不应并行: %d", workers)
	}
	if workers := mergeVerifyWorkers(2 * time.Hour); workers < 2 || workers > 6 {
		t.Fatalf("长片并行度超出范围: %d", workers)
	}
}

func TestMergeWorkerCountLimits(t *testing.T) {
	if workers := mergeWorkerCount(0); workers != 1 {
		t.Fatalf("无任务时并行度应为 1: %d", workers)
	}
	if workers := mergeWorkerCount(100); workers < 1 || workers > 4 {
		t.Fatalf("并行度超出范围: %d", workers)
	}
	if threads := mergeJobThreads(mergeWorkerCount(100)); threads < 1 {
		t.Fatalf("单任务线程数必须 >= 1: %d", threads)
	}
}
