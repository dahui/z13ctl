package cli

// parse.go — input parsing and validation helpers shared by cmd/ and the
// daemon: colors, brightness, fan curves, TDP flag resolution, and custom
// profile names. Everything here is pure — no sysfs, no device access — which
// is what lets both the CLI and the daemon validate a request identically
// before either touches hardware.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/aura"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// ParseColor parses a color name or 6-digit hex string (RRGGBB) into R, G, B
// bytes. Forwards to aura.ParseColor, where the color table lives.
func ParseColor(s string) (r, g, b uint8, err error) {
	return aura.ParseColor(s)
}

// ParseBrightness parses a brightness level name or number into a 0–3 uint8.
// Accepted names: off (0), low (1), medium (2), high (3).
// Numeric strings "0"–"3" are also accepted.
func ParseBrightness(s string) (uint8, error) {
	switch strings.ToLower(s) {
	case "off", "0":
		return 0, nil
	case "low", "1":
		return 1, nil
	case "medium", "med", "2":
		return 2, nil
	case "high", "3":
		return 3, nil
	}
	return 0, fmt.Errorf("brightness must be off/low/medium/high (or 0–3), got %q", s)
}

// ParseFanCurve parses a comma-separated curve string of temp:speed pairs
// against the device's fan-curve shape: the shape's Points is the required
// point count and its PWMMax the speed ceiling. Temperatures are Celsius and
// must be strictly increasing; speeds must be non-decreasing. PWM values may
// use a % suffix for percentage (0–100%), converted against PWMMax (80% = 204
// at the usual 255). Both formats can be mixed in the same curve string.
//
// Temperature bounds stay a fixed 0–120: the shape's TempMin/TempMax are the
// editor's display axis, not validation bounds — the Z13's axis starts at
// 35 °C while curves with 30 °C points have always been legal and are sitting
// in users' saved state.
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
		if isPercent {
			pwmStr = strings.TrimSuffix(pwmStr, "%")
		}
		pwm, err := strconv.Atoi(pwmStr)
		if err != nil {
			return nil, fmt.Errorf("invalid pwm in point %d: %w", i+1, err)
		}
		if temp < 0 || temp > 120 {
			return nil, fmt.Errorf("temp %d in point %d out of range 0–120", temp, i+1)
		}
		if isPercent {
			if pwm < 0 || pwm > 100 {
				return nil, fmt.Errorf("percentage %d in point %d out of range 0–100", pwm, i+1)
			}
			pwm = pwm * shape.PWMMax / 100
		} else if pwm < 0 || pwm > shape.PWMMax {
			return nil, fmt.Errorf("pwm %d in point %d out of range 0–%d", pwm, i+1, shape.PWMMax)
		}
		if i > 0 && temp <= points[i-1].Temp {
			return nil, fmt.Errorf("temps must be monotonically increasing: point %d (%d) <= point %d (%d)", i+1, temp, i, points[i-1].Temp)
		}
		if i > 0 && pwm < points[i-1].PWM {
			return nil, fmt.Errorf("pwm values must be non-decreasing: point %d (%d) < point %d (%d)", i+1, pwm, i, points[i-1].PWM)
		}
		points[i] = api.FanCurvePoint{Temp: temp, PWM: pwm}
	}
	return points, nil
}

// TDPStateFor resolves a unified watts value plus optional per-limit overrides
// into the five PPT values. pl1/pl2/pl3 override watts when non-zero; APU sPPT
// and Platform sPPT always follow PL2.
//
// Exposed separately from any write path so callers can hand the resolved
// state to the safety engine's ApplyTDPSafely, which needs to know PL1 before
// deciding whether the fan floor applies.
func TDPStateFor(watts, pl1, pl2, pl3 int) api.TDPState {
	if pl1 == 0 {
		pl1 = watts
	}
	if pl2 == 0 {
		pl2 = watts
	}
	if pl3 == 0 {
		pl3 = watts
	}
	return api.TDPState{
		PL1SPL:       pl1,
		PL2SPPT:      pl2,
		FPPT:         pl3,
		APUSPPT:      pl2,
		PlatformSPPT: pl2,
	}
}

// ValidateProfileName checks a user-supplied custom profile name.
//
// The rules moved to api.ValidateProfileName so that socket clients (the GUI,
// Decky) can pre-check a name with the same code the daemon refuses it with;
// this wrapper remains because everything daemon- and CLI-side reaches the
// rules through cli. The reasoning behind the reservations is documented on
// the api function.
func ValidateProfileName(name string) error {
	return api.ValidateProfileName(name)
}
