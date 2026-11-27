# Setup leaves a node at mode 0000 alone

**Setup leaves a node at mode 0000 alone** (`perms.hidden`), input or hidraw,
grant or revoke. No kernel creates one; InputPlumber hides the nodes it
manages that way, and a chmod would undo that for the whole group until the
next udev event. The udev rule cannot (InputPlumber's assignment is `MODE:=`,
final). A revoke once reset a hidden node to 0600 in a test; the rule now
covers revokes too.
