package safety

// curve.go — what a legal fan curve is, written once: the user's curve
// (ParseFanCurve, from the CLI and the daemon's fancurve command) and the
// device file's own curves (its presets and floor curve, checked in
// device.Config.Validate) are held to the same rules.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// CheckCurve reports the first thing wrong with a curve's points against a
// PWM ceiling: a temperature outside 0–120 °C, a PWM outside 0–pwmMax,
// temperatures that do not strictly increase, or PWMs that decrease. The
// point count is the caller's rule (a curve the user writes must match the
// fan shape; a floor curve may have any number).
func CheckCurve(pts []api.FanCurvePoint, pwmMax int) error {
	for i, p := range pts {
		if p.Temp < 0 || p.Temp > 120 {
			return fmt.Errorf("temp %d in point %d out of range 0–120", p.Temp, i+1)
		}
		if p.PWM < 0 || p.PWM > pwmMax {
			return fmt.Errorf("pwm %d in point %d out of range 0–%d", p.PWM, i+1, pwmMax)
		}
		if i > 0 && p.Temp <= pts[i-1].Temp {
			return fmt.Errorf("temps must be monotonically increasing: point %d (%d) <= point %d (%d)", i+1, p.Temp, i, pts[i-1].Temp)
		}
		if i > 0 && p.PWM < pts[i-1].PWM {
			return fmt.Errorf("pwm values must be non-decreasing: point %d (%d) < point %d (%d)", i+1, p.PWM, i, pts[i-1].PWM)
		}
	}
	return nil
}

// ParseFanCurve parses "temp:pwm,..." (or "temp:pct%") into exactly
// shape.Points points and checks them (CheckCurve). A percentage is scaled to
// the shape's PWM ceiling.
func ParseFanCurve(shape driver.FanShape, s string) ([]api.FanCurvePoint, error) {
	if shape.Points <= 0 || shape.PWMMax <= 0 {
		return nil, fmt.Errorf("device declares no fan curve shape")
	}
	parts := strings.Split(s, ",")
	if len(parts) != shape.Points {
		return nil, fmt.Errorf("fan curve must have exactly %d points, got %d", shape.Points, len(parts))
	}
	points := make([]api.FanCurvePoint, shape.Points)
	for i, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid curve point %q: expected temp:pwm or temp:pct%%", part)
		}
		temp, err := strconv.Atoi(strings.TrimSpace(kv[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid temp in point %d: %w", i+1, err)
		}
		pwmStr := strings.TrimSpace(kv[1])
		isPercent := strings.HasSuffix(pwmStr, "%")
		pwm, err := strconv.Atoi(strings.TrimSuffix(pwmStr, "%"))
		if err != nil {
			return nil, fmt.Errorf("invalid pwm in point %d: %w", i+1, err)
		}
		if isPercent {
			if pwm < 0 || pwm > 100 {
				return nil, fmt.Errorf("percentage %d in point %d out of range 0–100", pwm, i+1)
			}
			pwm = pwm * shape.PWMMax / 100
		}
		points[i] = api.FanCurvePoint{Temp: temp, PWM: pwm}
	}
	if err := CheckCurve(points, shape.PWMMax); err != nil {
		return nil, err
	}
	return points, nil
}
