// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package togglegate

import "time"

// Accept suppresses duplicate gui-toggle bursts.
// A single physical Armoury Crate button press has been observed to reach the
// GUI twice within the same instant; keep the first event and drop the burst.
func Accept(last, now time.Time, window time.Duration) (time.Time, bool) {
	if !last.IsZero() && now.Sub(last) < window {
		return last, false
	}
	return now, true
}
