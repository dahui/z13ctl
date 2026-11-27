package aura_test

import (
	"bytes"
	"testing"

	"github.com/dahui/voltaire/v2/internal/aura"
)

// mockWriter captures all Write calls, zero-padding each to 64 bytes.
type mockWriter struct {
	buf bytes.Buffer
}

func (m *mockWriter) Write(data []byte) error {
	padded := make([]byte, 64)
	copy(padded, data)
	m.buf.Write(padded)
	return nil
}

// containsPacket checks that buf contains a 64-byte packet starting with prefix.
func containsPacket(buf, prefix []byte) bool {
	const size = 64
	for i := 0; i+size <= len(buf); i += size {
		pkt := buf[i : i+size]
		if bytes.HasPrefix(pkt, prefix) {
			return true
		}
	}
	return false
}

func TestInit(t *testing.T) {
	t.Parallel()
	m := &mockWriter{}

	if err := aura.Init(m, aura.Z13); err != nil {
		t.Fatalf("Init: %v", err)
	}
	got := m.buf.Bytes()

	// Packet 1: 0x5d 0xB9
	if !containsPacket(got, []byte{0x5d, 0xB9}) {
		t.Error("Init: missing init packet 1 (0x5d 0xB9)")
	}
	// Packet 2: "]ASUS Tech.Inc." (0x5D = ']')
	if !containsPacket(got, []byte("]ASUS Tech.Inc.")) {
		t.Error("Init: missing init packet 2 (ASUS string)")
	}
	// Packet 3: 0x5d 0x05 0x20 0x31 0x00 0x1A
	if !containsPacket(got, []byte{0x5d, 0x05, 0x20, 0x31, 0x00, 0x1A}) {
		t.Error("Init: missing init packet 3")
	}
	// Packet 4: 0x5d 0xC0 0x03 0x01 (Z13 dynamic lighting)
	if !containsPacket(got, []byte{0x5d, 0xC0, 0x03, 0x01}) {
		t.Error("Init: missing init packet 4 (Z13 dyn lighting)")
	}
}

func TestSetPower_On(t *testing.T) {
	t.Parallel()
	m := &mockWriter{}

	if err := aura.SetPower(m, true); err != nil {
		t.Fatalf("SetPower(true): %v", err)
	}

	// Power ON: 0x5d 0xBD 0x01 0xFF 0x1F 0xFF 0xFF 0xFF
	if !containsPacket(m.buf.Bytes(), []byte{0x5d, 0xBD, 0x01, 0xFF, 0x1F, 0xFF, 0xFF, 0xFF}) {
		t.Error("SetPower(true): missing power-on packet")
	}
}

func TestSetPower_Off(t *testing.T) {
	t.Parallel()
	m := &mockWriter{}

	if err := aura.SetPower(m, false); err != nil {
		t.Fatalf("SetPower(false): %v", err)
	}

	// Power OFF: 0x5d 0xBD 0x01 0x00 0x00 0x00 0x00 0xFF
	if !containsPacket(m.buf.Bytes(), []byte{0x5d, 0xBD, 0x01, 0x00, 0x00, 0x00, 0x00, 0xFF}) {
		t.Error("SetPower(false): missing power-off packet")
	}
}

func TestSetBrightness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level    uint8
		wantByte uint8
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 3},
		{5, 3}, // clamped to 3
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			m := &mockWriter{}

			if err := aura.SetBrightness(m, tt.level); err != nil {
				t.Fatalf("SetBrightness(%d): %v", tt.level, err)
			}

			// 0x5d 0xBA 0xC5 0xC4 <level>
			if !containsPacket(m.buf.Bytes(), []byte{0x5d, 0xBA, 0xC5, 0xC4, tt.wantByte}) {
				t.Errorf("SetBrightness(%d): wrong packet (want level byte %d)", tt.level, tt.wantByte)
			}
		})
	}
}

func TestSetMode_Static(t *testing.T) {
	t.Parallel()
	m := &mockWriter{}

	err := aura.SetMode(m, 0, aura.ModeStatic, 0xFF, 0x00, 0x00, 0, 0, 0, aura.SpeedNormal)
	if err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	got := m.buf.Bytes()

	// SetMode packet: 0x5d 0xB3 zone mode r g b speed dir randFlag r2 g2 b2
	if !containsPacket(got, []byte{0x5d, 0xB3, 0x00, 0x00, 0xFF, 0x00, 0x00}) {
		t.Error("SetMode static: missing mode packet")
	}
	// commit MESSAGE_SET: 0x5d 0xB5
	if !containsPacket(got, []byte{0x5d, 0xB5}) {
		t.Error("SetMode: missing MESSAGE_SET commit")
	}
	// commit MESSAGE_APPLY: 0x5d 0xB4
	if !containsPacket(got, []byte{0x5d, 0xB4}) {
		t.Error("SetMode: missing MESSAGE_APPLY commit")
	}
}

func TestSetMode_RandomColor(t *testing.T) {
	t.Parallel()
	m := &mockWriter{}

	// Zero RGB → randFlag = 0xFF
	err := aura.SetMode(m, 0, aura.ModeCycle, 0, 0, 0, 0, 0, 0, aura.SpeedNormal)
	if err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	got := m.buf.Bytes()

	// Packet: 5d B3 zone mode r g b speed dir randFlag r2 g2 b2
	// When r=g=b=0, randFlag=0xFF
	if !containsPacket(got, []byte{0x5d, 0xB3, 0x00, byte(aura.ModeCycle), 0x00, 0x00, 0x00, byte(aura.SpeedNormal), 0x00, 0xFF}) {
		t.Error("SetMode cycle with zero color: randFlag should be 0xFF")
	}
}

func TestSetMode_Breathe(t *testing.T) {
	t.Parallel()
	m := &mockWriter{}

	// Breathe with non-zero color → randFlag = 0x01
	err := aura.SetMode(m, 0, aura.ModeBreathe, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xFF, aura.SpeedNormal)
	if err != nil {
		t.Fatalf("SetMode breathe: %v", err)
	}
	got := m.buf.Bytes()

	// randFlag = 0x01 for breathe with non-zero primary color
	if !containsPacket(got, []byte{0x5d, 0xB3, 0x00, byte(aura.ModeBreathe), 0xFF, 0x00, 0x00, byte(aura.SpeedNormal), 0x00, 0x01}) {
		t.Error("SetMode breathe: randFlag should be 0x01")
	}
}

func TestApply(t *testing.T) {
	t.Parallel()
	m := &mockWriter{}

	err := aura.Apply(m, aura.Z13, []uint8{0, 1}, aura.ModeStatic, 0x00, 0xFF, 0x00, 0, 0, 0, aura.SpeedNormal, 2)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := m.buf.Bytes()

	// Must contain Init, Power ON, Brightness, SetMode for both zones.
	if !containsPacket(got, []byte{0x5d, 0xB9}) {
		t.Error("Apply: missing Init packet 1")
	}
	if !containsPacket(got, []byte{0x5d, 0xBD, 0x01, 0xFF}) {
		t.Error("Apply: missing Power ON packet")
	}
	if !containsPacket(got, []byte{0x5d, 0xBA, 0xC5, 0xC4, 0x02}) {
		t.Error("Apply: missing Brightness packet (level 2)")
	}
	// Zone 0 SetMode
	if !containsPacket(got, []byte{0x5d, 0xB3, 0x00}) {
		t.Error("Apply: missing SetMode for zone 0")
	}
	// Zone 1 SetMode
	if !containsPacket(got, []byte{0x5d, 0xB3, 0x01}) {
		t.Error("Apply: missing SetMode for zone 1")
	}
}

func TestTurnOff(t *testing.T) {
	t.Parallel()
	m := &mockWriter{}

	if err := aura.TurnOff(m, aura.Z13); err != nil {
		t.Fatalf("TurnOff: %v", err)
	}
	got := m.buf.Bytes()

	// Init + Power OFF + Brightness 0
	if !containsPacket(got, []byte{0x5d, 0xB9}) {
		t.Error("TurnOff: missing Init packet")
	}
	if !containsPacket(got, []byte{0x5d, 0xBD, 0x01, 0x00, 0x00, 0x00, 0x00, 0xFF}) {
		t.Error("TurnOff: missing Power OFF packet")
	}
	if !containsPacket(got, []byte{0x5d, 0xBA, 0xC5, 0xC4, 0x00}) {
		t.Error("TurnOff: missing Brightness 0 packet")
	}
}

// TestBrightnessNamesCoverEveryLevel keeps the level names in step with the
// scale they name: one per level, 0 through aura.MaxBrightness.
func TestBrightnessNamesCoverEveryLevel(t *testing.T) {
	if len(aura.BrightnessNames) != aura.MaxBrightness+1 {
		t.Fatalf("%d brightness names for levels 0–%d", len(aura.BrightnessNames), aura.MaxBrightness)
	}
	if aura.BrightnessNames[0] != "off" {
		t.Errorf("level 0 is named %q; it is the off level", aura.BrightnessNames[0])
	}
}

// ally is the ROG Xbox Ally X's protocol (G-Helper's Ally path): no Z13
// Dynamic Lighting packet and no 0xBD power message.
var ally = aura.Protocol{}

func TestAllyProtocolSendsOnlyItsPackets(t *testing.T) {
	m := &mockWriter{}
	if err := aura.Apply(m, ally, []uint8{0}, aura.ModeBreathe, 0xFF, 0x80, 0x00, 0, 0, 0xFF, aura.SpeedNormal, 3); err != nil {
		t.Fatal(err)
	}
	got := m.buf.Bytes()
	for _, want := range [][]byte{
		{0x5d, 0xB9},                                                 // init
		[]byte("]ASUS Tech.Inc."),                                    // init
		{0x5d, 0x05, 0x20, 0x31, 0x00, 0x1A},                         // init
		{0x5d, 0xBA, 0xC5, 0xC4, 0x03},                               // brightness
		{0x5d, 0xB3, 0x00, byte(aura.ModeBreathe), 0xFF, 0x80, 0x00}, // zone 0: every ring
		{0x5d, 0xB5, 0x00, 0x00, 0x00}, {0x5d, 0xB4},                 // set, apply
	} {
		if !containsPacket(got, want) {
			t.Errorf("missing packet % x", want)
		}
	}
	// Only packets G-Helper sends the Ally: never the Z13's.
	if containsPacket(got, []byte{0x5d, 0xC0}) || containsPacket(got, []byte{0x5d, 0xBD}) {
		t.Error("the Ally was sent a Z13-only packet (0xC0 init or 0xBD power)")
	}

	m = &mockWriter{}
	if err := aura.TurnOff(m, ally); err != nil {
		t.Fatal(err)
	}
	if containsPacket(m.buf.Bytes(), []byte{0x5d, 0xBD}) || !containsPacket(m.buf.Bytes(), []byte{0x5d, 0xBA, 0xC5, 0xC4, 0x00}) {
		t.Error("the Ally's off must be brightness 0 with no 0xBD power message")
	}
}
