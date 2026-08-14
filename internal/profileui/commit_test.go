// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

import "testing"

func TestCommitLabel(t *testing.T) {
	if got := CommitLabel(true); got != "Apply Changes" {
		t.Errorf("live: got %q", got)
	}
	if got := CommitLabel(false); got != "Save Changes" {
		t.Errorf("stored: got %q", got)
	}
}

func TestUnsavedSummary(t *testing.T) {
	cases := []struct {
		name          string
		tdp, fans, uv bool
		want          string
	}{
		{"clean", false, false, false, ""},
		{"tdp only", true, false, false, "Unsaved: TDP"},
		{"fans only", false, true, false, "Unsaved: fan curve"},
		{"uv only", false, false, true, "Unsaved: undervolt"},
		{"tdp and fans", true, true, false, "Unsaved: TDP · fan curve"},
		{"all three", true, true, true, "Unsaved: TDP · fan curve · undervolt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnsavedSummary(tc.tdp, tc.fans, tc.uv); got != tc.want {
				t.Errorf("UnsavedSummary(%v, %v, %v) = %q, want %q",
					tc.tdp, tc.fans, tc.uv, got, tc.want)
			}
		})
	}
}
