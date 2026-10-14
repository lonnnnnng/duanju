package app

import "testing"

func TestMergeThreadCountFor(t *testing.T) {
	tests := []struct {
		procs int
		want  int
	}{
		{0, 2},
		{1, 2},
		{2, 2},
		{4, 4},
		{8, 8},
		{16, 8},
	}
	for _, test := range tests {
		if got := mergeThreadCountFor(test.procs); got != test.want {
			t.Fatalf("mergeThreadCountFor(%d) = %d, want %d", test.procs, got, test.want)
		}
	}
}
