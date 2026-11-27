package dismiss

import "testing"

func TestBackClosesTheInnermostFirst(t *testing.T) {
	all := State{Editing: true, PopupOpen: true, FullVisible: true, SubView: true, DrawerVisible: true}
	for _, tc := range []struct {
		name string
		s    State
		want Action
	}{
		{"editing wins over everything", all, ExitEdit},
		{"a popup closes before its surface", State{PopupOpen: true, FullVisible: true, DrawerVisible: true}, ClosePopup},
		{"the full window before the drawer", State{FullVisible: true, SubView: true, DrawerVisible: true}, HideFull},
		{"a sub-page goes back to main", State{SubView: true, DrawerVisible: true}, BackToMain},
		{"then the drawer closes", State{DrawerVisible: true}, HideDrawer},
	} {
		if got := Back(tc.s); got != tc.want {
			t.Errorf("%s: Back = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestToggle(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    State
		want Action
	}{
		{"the full window wins", State{FullVisible: true, DrawerVisible: true}, HideFull},
		{"the drawer", State{DrawerVisible: true, PopupOpen: true}, HideDrawer},
		{"nothing up: the caller opens", State{}, None},
	} {
		if got := Toggle(tc.s); got != tc.want {
			t.Errorf("%s: Toggle = %v, want %v", tc.name, got, tc.want)
		}
	}
}
