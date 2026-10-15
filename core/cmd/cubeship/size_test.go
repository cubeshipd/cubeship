package main

import "testing"

func TestASizeIsReadTheWaySomebodyWritesOne(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"512", 512},
		{"512B", 512},
		{"512Mi", 512 << 20},
		// The same request from somebody who does not spell out the
		// binary unit. Refusing it would be pedantry in front of a
		// limit.
		{"512M", 512 << 20},
		{"2Gi", 2 << 30},
		{"1.5Gi", 3 << 29},
		{" 256mi ", 256 << 20},
		{"0", 0},
	} {
		got, err := parseSize(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q read as %d, want %d", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "big", "Mi", "12x"} {
		if _, err := parseSize(bad); err == nil {
			t.Errorf("%q was accepted as a size", bad)
		}
	}
}

func TestASizeComesBackTheWayItWentIn(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{512 << 20, "512Mi"},
		{2 << 30, "2Gi"},
		{512, "512B"},
	} {
		if got := formatSize(tc.in); got != tc.want {
			t.Errorf("%d printed as %q, want %q", tc.in, got, tc.want)
		}
	}
}
