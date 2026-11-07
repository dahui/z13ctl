package daemon

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dahui/z13ctl/api"
)

// stubVRAM stands in for the z13ctl-vram helper.
func stubVRAM(t *testing.T, get func() (api.VRAMState, error), set func(string) (api.VRAMState, error)) {
	t.Helper()
	prevGet, prevSet := vramGet, vramSet
	vramGet, vramSet = get, set
	t.Cleanup(func() { vramGet, vramSet = prevGet, prevSet })
}

func TestVRAMGetRelaysHelperState(t *testing.T) {
	stubVRAM(t, func() (api.VRAMState, error) {
		return api.VRAMState{Current: "8G", Next: "0.5G", Options: []string{"auto", "0.5G", "8G"}}, nil
	}, nil)

	resp := (&Daemon{}).dispatch(request{Cmd: "vram-get"})
	if !resp.OK {
		t.Fatalf("vram-get = %+v", resp)
	}
	var st api.VRAMState
	if err := json.Unmarshal([]byte(resp.Value), &st); err != nil {
		t.Fatalf("value %q: %v", resp.Value, err)
	}
	if st.Current != "8G" || st.Next != "0.5G" || len(st.Options) != 3 {
		t.Errorf("state = %+v", st)
	}
}

func TestVRAMSetPassesSizeAndSurfacesErrors(t *testing.T) {
	var asked []string
	stubVRAM(t, nil, func(size string) (api.VRAMState, error) {
		asked = append(asked, size)
		if size == "3G" {
			return api.VRAMState{}, errors.New("3G is not an offered size")
		}
		return api.VRAMState{Current: "8G", Next: size}, nil
	})
	d := &Daemon{}

	if resp := d.dispatch(request{Cmd: "vram", Set: "1G"}); !resp.OK {
		t.Errorf("vram 1G = %+v", resp)
	}
	if resp := d.dispatch(request{Cmd: "vram", Set: "3G"}); resp.OK || resp.Error == "" {
		t.Errorf("vram 3G = %+v, want the helper's error", resp)
	}
	if resp := d.dispatch(request{Cmd: "vram"}); resp.OK {
		t.Errorf("vram with no size = %+v, want an error", resp)
	}
	if len(asked) != 2 || asked[0] != "1G" || asked[1] != "3G" {
		t.Errorf("helper asked for %v, want [1G 3G] (an empty size never reaches it)", asked)
	}
}
