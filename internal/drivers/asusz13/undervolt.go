package asusz13

// undervolt.go — AMD Curve Optimizer (CO) control via the ryzen_smu kernel module.
//
// Curve Optimizer adjusts the voltage-frequency curve for all CPU cores. Negative
// values reduce voltage (undervolt), improving efficiency and thermals. Values are
// volatile — they reset on reboot, sleep, or profile change.
//
// The command is the CPU's, not the device's: which SMU message sets an all-core
// offset depends on the silicon, and a message sent to the wrong one is still an
// MP1 mailbox write (see the 2026-08-14 lockup in Daemon.uvApplied). The table
// below is therefore keyed on the CPUID family and model, and a CPU it does not
// list is refused before anything — the probe included — touches the mailbox.

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// coPlatform is one CPU's all-core Curve Optimizer command.
type coPlatform struct {
	Name    string
	Mailbox string
	Cmd     uint32
	// ForkNote is the hint for an "unknown command" answer, where one is known:
	// the ryzen_smu fork this silicon needs.
	ForkNote string
}

// cpuModel is the CPUID family and model, as /proc/cpuinfo prints them.
type cpuModel struct{ family, model int }

// coPlatforms is ryzenadj 0.19.0's set_coall (lib/api.c), keyed by its CPUID
// table (lib/cpuid.c), transcribed rather than recalled. Only the Strix Halo
// row has run here (the Z13). Dragon Range and Fire Range are left out: ryzenadj
// sends them PSMU 0x7, and which ryzen_smu mailbox file is its PSMU has not been
// checked. iGPU CO (set_cogfx) is not offered at all — ryzenadj itself excludes
// Strix Halo from it ("0xB7 is rejected on this architecture").
var coPlatforms = map[cpuModel]coPlatform{
	{0x17, 96}:  {Name: "Renoir", Mailbox: MailboxMP1, Cmd: 0x55},
	{0x17, 104}: {Name: "Lucienne", Mailbox: MailboxMP1, Cmd: 0x55},
	{0x17, 144}: {Name: "Van Gogh", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x17, 145}: {Name: "Van Gogh", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x19, 80}:  {Name: "Cezanne", Mailbox: MailboxMP1, Cmd: 0x55},
	{0x19, 64}:  {Name: "Rembrandt", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x19, 68}:  {Name: "Rembrandt", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x19, 116}: {Name: "Phoenix", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x19, 120}: {Name: "Phoenix", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x19, 117}: {Name: "Hawk Point", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x1A, 32}:  {Name: "Strix Point", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x1A, 36}:  {Name: "Strix Point", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x1A, 96}:  {Name: "Krackan Point", Mailbox: MailboxMP1, Cmd: 0x4C},
	{0x1A, 112}: {Name: "Strix Halo", Mailbox: MailboxMP1, Cmd: 0x4C,
		ForkNote: "the amkillam/ryzen_smu fork is required (leogx9r's does not support Strix Halo)"},
}

// readCPUModel reads the first CPU's vendor, family and model from cpuinfo.
func readCPUModel() (vendor string, m cpuModel, err error) {
	data, err := os.ReadFile(procCPUInfoPath)
	if err != nil {
		return "", m, err
	}
	var haveFamily, haveModel bool
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			if vendor != "" {
				break // a blank line ends the first processor's block
			}
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "vendor_id":
			vendor = val
		case "cpu family":
			m.family, err = strconv.Atoi(val)
			haveFamily = err == nil
		case "model":
			m.model, err = strconv.Atoi(val)
			haveModel = err == nil
		}
	}
	if !haveFamily || !haveModel {
		return vendor, m, fmt.Errorf("%s: no cpu family/model", procCPUInfoPath)
	}
	return vendor, m, nil
}

// coPlatformFor returns this CPU's Curve Optimizer command, or an error naming
// the CPU when it has none this driver knows.
func coPlatformFor() (coPlatform, error) {
	vendor, m, err := readCPUModel()
	if err != nil {
		return coPlatform{}, fmt.Errorf("reading the CPU model: %w", err)
	}
	if vendor != "AuthenticAMD" {
		return coPlatform{}, fmt.Errorf("curve optimizer is an AMD feature; this CPU is %q", vendor)
	}
	p, ok := coPlatforms[m]
	if !ok {
		return coPlatform{}, fmt.Errorf("no known Curve Optimizer command for AMD family 0x%X model %d", m.family, m.model)
	}
	return p, nil
}

// CurveOptimizerCommand names the SMU message an offset write would send on
// this CPU, for the dry run — or the reason none would be sent.
func CurveOptimizerCommand() (string, error) {
	p, err := coPlatformFor()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s cmd 0x%X (%s)", mailboxLabel(p.Mailbox), p.Cmd, p.Name), nil
}

// mailboxLabel is a mailbox file's name as ryzenadj and the docs write it.
func mailboxLabel(mailbox string) string {
	if mailbox == MailboxMP1 {
		return "MP1"
	}
	return strings.ToUpper(strings.TrimSuffix(mailbox, "_cmd"))
}

// encodeCOValue encodes a Curve Optimizer offset for the SMU.
// Input is a non-positive integer (e.g. -20 for 20mV undervolt).
// Encoding: 0x100000 - abs(value).
func encodeCOValue(offset int) uint32 {
	if offset >= 0 {
		return 0x100000
	}
	return uint32(0x100000) - uint32(-offset)
}

// coResponseError is smuResponseError with the platform's fork hint on an
// "unknown command" answer — the only guidance a user with the wrong fork gets.
func coResponseError(p coPlatform, code uint32) error {
	if code == SMUReturnUnknownCmd {
		if p.ForkNote != "" {
			return fmt.Errorf("SMU unknown command (0xFE) on %s — %s", p.Name, p.ForkNote)
		}
		return fmt.Errorf("SMU unknown command (0xFE) — the installed ryzen_smu does not support Curve Optimizer on %s", p.Name)
	}
	return smuResponseError(code)
}

// sendCO writes one all-core offset with this CPU's command. Callers have
// already probed, which refused an unknown CPU without writing.
func sendCO(offset int) error {
	p, err := coPlatformFor()
	if err != nil {
		return err
	}
	resp, _, err := SendSMUCommand(p.Mailbox, p.Cmd, [6]uint32{encodeCOValue(offset)})
	if err != nil {
		return err
	}
	return coResponseError(p, resp)
}

// SetCurveOptimizer applies a Curve Optimizer offset to all CPU cores.
// The value must be <= 0. A value of 0 means "stock" (no change).
//
// The offset bounds live in device data and are enforced by the undervolter
// driver's Apply before this runs; encodeCOValue independently maps any
// positive value to stock, so an overvolt cannot be encoded here at all.
func SetCurveOptimizer(cpuOffset int) error {
	if !SMUProbeUndervolt() {
		return errCONotAvailable()
	}
	if err := sendCO(cpuOffset); err != nil {
		return fmt.Errorf("CPU CO: %w", err)
	}
	return nil
}

// ResetCurveOptimizer resets the CPU Curve Optimizer to stock (0).
func ResetCurveOptimizer() error {
	if !SMUProbeUndervolt() {
		return errCONotAvailable()
	}
	if err := sendCO(0); err != nil {
		return fmt.Errorf("reset CPU CO: %w", err)
	}
	return nil
}

// errCONotAvailable says why the probe refused: an unknown CPU names itself,
// anything else is the module or the fork.
func errCONotAvailable() error {
	if _, err := coPlatformFor(); err != nil {
		return fmt.Errorf("curve optimizer not available — %w", err)
	}
	return fmt.Errorf("curve optimizer not available — ryzen_smu module missing or does not support this platform")
}
