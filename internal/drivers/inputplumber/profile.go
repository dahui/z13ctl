// Package inputplumber reaches hardware buttons that InputPlumber manages.
//
// InputPlumber (ShadowBlip's input daemon, which handheld distributions ship)
// takes a device over completely: it EVIOCGRABs its evdev nodes and sets them
// and its hidraw nodes to mode 0000, so voltaire's direct button drivers
// cannot see a press on a device it manages. What it offers instead is a
// profile: each composite device maps capabilities (Gamepad:Button:Keyboard…)
// to target events, and a "dbus: <name>" target is emitted as an
// org.shadowblip.Input.DBusDevice.InputEvent signal on the system bus, which an
// unprivileged client receives (verified on a OneXPlayer X2 Mini Pro,
// InputPlumber 0.84.0, 2026-11-17).
//
// So the route has two halves, both here: a profile edit that maps one
// capability to voltaire's action (SetMapping, pure), and a watcher that turns
// that action's InputEvent into a button press (Buttons). Deciding when to
// edit which device's profile is the daemon's.
//
// voltaire never writes an InputPlumber file. A profile is loaded over DBus
// into the running daemon only; InputPlumber starts every composite device
// on its default profile again when it restarts, which is why the daemon
// re-applies the mapping.
package inputplumber

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ActionPrefix starts every DBus action voltaire maps, and marks a profile
// mapping as voltaire's: a mapping with a dbus target under this prefix is
// one SetMapping wrote, and the only kind it removes.
const ActionPrefix = "voltaire_"

// Action is the DBus action name for a button ID.
func Action(buttonID string) string { return ActionPrefix + buttonID }

// ButtonID is the button an action names, ok false for an action that is not
// voltaire's.
func ButtonID(action string) (string, bool) {
	id, ok := strings.CutPrefix(action, ActionPrefix)
	return id, ok && id != ""
}

// replacedPrefix starts the name of a voltaire mapping that took a capability
// over from a mapping already in the profile; the rest of the name is that
// mapping in YAML flow style, so removing voltaire's mapping can put it back
// even after a daemon restart. The name is free text to InputPlumber.
const replacedPrefix = "voltaire, replacing: "

// SetMapping returns profile (InputPlumber DeviceProfile YAML, as
// GetProfileYaml returns it) with exactly one voltaire mapping: the gamepad
// button capability to the DBus action. An empty capability removes
// voltaire's mappings instead. changed is false when the profile already says
// that, so the caller loads nothing and a re-check never writes.
//
// A mapping the profile already has for the same capability is replaced, not
// joined: both would fire, and the button would do its old job too (on the
// OXP, Screenshot). The replaced mapping is kept in voltaire's mapping's name
// and restored when voltaire's mapping is removed. Everything else in the
// profile, fields voltaire does not know included, is kept as it was.
func SetMapping(profile []byte, capability, action string) (out []byte, changed bool, err error) {
	if capability != "" && !strings.HasPrefix(action, ActionPrefix) {
		return nil, false, fmt.Errorf("action %q is not voltaire's (prefix %q)", action, ActionPrefix)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(profile, &doc); err != nil {
		return nil, false, fmt.Errorf("parsing profile: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, errors.New("profile is not a YAML mapping")
	}
	root := doc.Content[0]
	seq := mapValue(root, "mapping")
	if seq == nil && capability == "" {
		return profile, false, nil
	}
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, scalar("mapping"), seq)
	}
	if seq.Kind != yaml.SequenceNode {
		return nil, false, errors.New("profile's mapping is not a list")
	}

	if capability != "" && alreadyMapped(seq, capability, action) {
		return profile, false, nil
	}

	// Take voltaire's mappings out, putting back whatever each had replaced.
	var kept []*yaml.Node
	for _, m := range seq.Content {
		if !isOurs(m) {
			kept = append(kept, m)
			continue
		}
		changed = true
		if orig := replacedMapping(m); orig != nil {
			kept = append(kept, orig)
		}
	}
	seq.Content = kept

	if capability != "" {
		ours := ourMapping(capability, action)
		at := len(seq.Content)
		for i, m := range seq.Content {
			if sourceButton(m) != capability {
				continue
			}
			name, err := flow(m)
			if err != nil {
				return nil, false, err
			}
			setMapValue(ours, "name", scalar(replacedPrefix+name))
			at = i
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			break
		}
		seq.Content = append(seq.Content[:at], append([]*yaml.Node{ours}, seq.Content[at:]...)...)
		changed = true
	}
	if !changed {
		return profile, false, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, false, fmt.Errorf("writing profile: %w", err)
	}
	return buf.Bytes(), true, nil
}

// alreadyMapped reports whether the profile maps capability to action through
// voltaire, and nothing else voltaire's or anyone's on that capability.
func alreadyMapped(seq *yaml.Node, capability, action string) bool {
	found := false
	for _, m := range seq.Content {
		src := sourceButton(m)
		switch {
		case isOurs(m) && src == capability && dbusTarget(m) == action:
			if found {
				return false // two of ours: tidy up
			}
			found = true
		case isOurs(m):
			return false // ours on another capability, or another action
		case src == capability:
			return false // someone else's mapping on our capability
		}
	}
	return found
}

// ourMapping builds voltaire's mapping: capability to one DBus action.
func ourMapping(capability, action string) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	gamepad := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMapValue(gamepad, "button", scalar(capability))
	source := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMapValue(source, "gamepad", gamepad)
	target := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMapValue(target, "dbus", scalar(action))
	setMapValue(m, "name", scalar("voltaire"))
	setMapValue(m, "source_event", source)
	setMapValue(m, "target_events", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{target}})
	return m
}

// isOurs reports whether a mapping is voltaire's: one of its targets is a
// dbus action under ActionPrefix.
func isOurs(m *yaml.Node) bool { return strings.HasPrefix(dbusTarget(m), ActionPrefix) }

// dbusTarget is the first dbus action among a mapping's targets, "" if none.
func dbusTarget(m *yaml.Node) string {
	targets := mapValue(m, "target_events")
	if targets == nil || targets.Kind != yaml.SequenceNode {
		return ""
	}
	for _, t := range targets.Content {
		if v := mapValue(t, "dbus"); v != nil && v.Kind == yaml.ScalarNode {
			return v.Value
		}
	}
	return ""
}

// sourceButton is the gamepad button a mapping's source event names, "" for
// any other kind of source.
func sourceButton(m *yaml.Node) string {
	if v := mapValue(mapValue(mapValue(m, "source_event"), "gamepad"), "button"); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}

// replacedMapping decodes the mapping a voltaire mapping's name carries, nil
// when it replaced nothing or the name does not parse (then there is nothing
// safe to restore).
func replacedMapping(m *yaml.Node) *yaml.Node {
	name := mapValue(m, "name")
	if name == nil {
		return nil
	}
	text, ok := strings.CutPrefix(name.Value, replacedPrefix)
	if !ok {
		return nil
	}
	var doc yaml.Node
	if yaml.Unmarshal([]byte(text), &doc) != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	orig := doc.Content[0]
	setStyle(orig, 0)
	return orig
}

// flow renders a mapping on one line, for keeping it in a name.
func flow(m *yaml.Node) (string, error) {
	c := deepCopy(m)
	setStyle(c, yaml.FlowStyle)
	out, err := yaml.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("keeping the replaced mapping: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setMapValue(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, scalar(key), v)
}

func scalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

func setStyle(n *yaml.Node, s yaml.Style) {
	if n.Kind != yaml.ScalarNode {
		n.Style = s
	}
	for _, c := range n.Content {
		setStyle(c, s)
	}
}

func deepCopy(n *yaml.Node) *yaml.Node {
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, k := range n.Content {
		c.Content[i] = deepCopy(k)
	}
	return &c
}
