package asusz13

// fan_enum_test.go — the fan hardware's shape is the kernel's answer: channels,
// points per curve and the fans that report a speed are enumerated from hwmon's
// attribute names, and the device data is checked against them.

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// seedCurve creates n curve points on each of chans, with pwm_enable on auto.
func (f *fakeSysfs) seedCurve(t *testing.T, chans []int, n int) {
	t.Helper()
	for _, ch := range chans {
		for i := 1; i <= n; i++ {
			f.writeFile(t, f.hwmon+"/pwm"+itoa(ch)+"_auto_point"+itoa(i)+"_temp", itoa(30+i))
			f.writeFile(t, f.hwmon+"/pwm"+itoa(ch)+"_auto_point"+itoa(i)+"_pwm", itoa(10*i))
		}
		f.writeFile(t, f.hwmon+"/pwm"+itoa(ch)+"_enable", "2")
	}
}

func ramp(n int) []api.FanCurvePoint {
	pts := make([]api.FanCurvePoint, n)
	for i := range pts {
		pts[i] = api.FanCurvePoint{Temp: 40 + 5*i, PWM: 20 * i}
	}
	return pts
}

// A curve device with six points on one channel takes a six-point curve on that
// channel and nothing else — no write to a channel the kernel did not list.
func TestFanCurvesFollowTheKernelsShape(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedCurve(t, []int{1}, 6)

	chans, n, err := FanCurveShape()
	if err != nil || !slices.Equal(chans, []int{1}) || n != 6 {
		t.Fatalf("FanCurveShape() = %v, %d, %v; want [1], 6", chans, n, err)
	}
	if werr := SetFanCurves(ramp(8)); werr == nil || !strings.Contains(werr.Error(), "exactly 6 points") {
		t.Errorf("an 8-point curve on 6-point hardware = %v, want refused", werr)
	}
	if werr := SetFanCurves(ramp(6)); werr != nil {
		t.Fatalf("SetFanCurves(6 points) = %v", werr)
	}
	if got := f.readInt(t, f.hwmon+"/pwm1_auto_point6_pwm"); got != 100 {
		t.Errorf("point 6 pwm = %d, want 100", got)
	}
	if f.exists(f.hwmon + "/pwm2_auto_point1_temp") {
		t.Error("wrote a curve to channel 2, which the kernel does not list")
	}
	curves, err := ReadFanCurves()
	if err != nil || len(curves) != 1 || !slices.Equal(curves[0], ramp(6)) {
		t.Errorf("ReadFanCurves() = %v, %v; want the 6 points written", curves, err)
	}
}

func TestFanCurveShapeRefusesDisagreeingChannels(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedCurve(t, []int{1}, 8)
	f.seedCurve(t, []int{2}, 6)
	if _, _, err := FanCurveShape(); err == nil {
		t.Error("channels with 8 and 6 points = nil error, want refused")
	}
	if err := SetFanCurves(ramp(8)); err == nil {
		t.Error("SetFanCurves on disagreeing channels = nil, want refused")
	}
}

// One fan reporting a speed is one reading, not a failed read of two.
func TestReadFanRPMsReadsTheFansThatExist(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.hwmonRead+"/fan1_input", "2400")
	rpms, err := ReadFanRPMs()
	if err != nil || !slices.Equal(rpms, []int{2400}) {
		t.Errorf("ReadFanRPMs() = %v, %v; want [2400]", rpms, err)
	}
}

func TestFanLabels(t *testing.T) {
	for _, tt := range []struct {
		name             string
		declared, kernel []string
		want             []string
	}{
		{"device data wins", []string{"Fan 1", "Fan 2"}, []string{"cpu_fan", "gpu_fan"}, []string{"Fan 1", "Fan 2"}},
		{"kernel label prettified", nil, []string{"cpu_fan", "mid_fan"}, []string{"CPU fan", "Mid fan"}},
		{"no label at all", nil, []string{"", ""}, []string{"Fan 1", "Fan 2"}},
		{"the kernel decides the count", []string{"Left"}, []string{"cpu_fan", "gpu_fan", ""}, []string{"Left", "GPU fan", "Fan 3"}},
		{"no kernel answer: the data alone", []string{"Left", "Right"}, nil, []string{"Left", "Right"}},
	} {
		if got := fanLabels(tt.declared, tt.kernel); !slices.Equal(got, tt.want) {
			t.Errorf("%s: fanLabels = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFanControllerShapeLaysTheKernelOverDeviceData(t *testing.T) {
	data := driver.FanShape{Points: 8, TempMin: 35, TempMax: 105, PWMMax: 255}

	f := newFakeSysfs(t)
	f.seedCurve(t, []int{1, 2}, 8)
	f.writeFile(t, f.hwmonRead+"/fan1_input", "0")
	f.writeFile(t, f.hwmonRead+"/fan1_label", "cpu_fan")
	f.writeFile(t, f.hwmonRead+"/fan2_input", "0")
	s := NewFanController(data).Shape()
	if s.Points != 8 || !slices.Equal(s.Labels, []string{"CPU fan", "Fan 2"}) {
		t.Errorf("agreeing kernel: Shape = %+v", s)
	}

	// Undeclared count: the kernel's.
	f2 := newFakeSysfs(t)
	f2.seedCurve(t, []int{1}, 6)
	if s := NewFanController(driver.FanShape{TempMin: 35, TempMax: 105}).Shape(); s.Points != 6 {
		t.Errorf("undeclared points = %d, want the kernel's 6", s.Points)
	}

	// Disagreeing count: the data's shape stands (presets and the floor are
	// sized for it) and curve writes fail closed against the kernel's count.
	if s := NewFanController(data).Shape(); s.Points != 8 {
		t.Errorf("mismatched points = %d, want the device data's 8", s.Points)
	}
	if err := NewFanController(data).ApplyCurve(ramp(8)); err == nil {
		t.Error("ApplyCurve on a kernel with a different point count = nil, want refused")
	}
}

func (f *fakeSysfs) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
