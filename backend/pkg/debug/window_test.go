package debug

import "testing"

func TestIsInWindow(t *testing.T) {
	const (
		start = int64(1000)
		end   = int64(2000)
	)

	cases := []struct {
		name     string
		now      int64
		s        int64
		e        int64
		expected bool
	}{
		// 窗口内
		{"now == start (lower bound)", start, start, end, true},
		{"now == end (upper bound)", end, start, end, true},
		{"now in middle", 1500, start, end, true},

		// 窗口外
		{"now before start", 999, start, end, false},
		{"now after end", 2001, start, end, false},

		// 零值防呆
		{"start == 0", 1500, 0, end, false},
		{"end == 0", 1500, start, 0, false},

		// 非法窗口
		{"end < start (reversed)", 1500, end, start, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsInWindow(tc.now, tc.s, tc.e)
			if got != tc.expected {
				t.Errorf("IsInWindow(now=%d, s=%d, e=%d) = %v, want %v",
					tc.now, tc.s, tc.e, got, tc.expected)
			}
		})
	}
}