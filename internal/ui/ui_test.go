package ui

import "testing"

func TestStylingToggle(t *testing.T) {
	defer SetEnabled(Enabled())

	SetEnabled(false)
	if got := Bold("x"); got != "x" {
		t.Errorf("with color off, Bold(x) = %q, want plain x", got)
	}

	SetEnabled(true)
	got := Red("err")
	if got == "err" {
		t.Error("with color on, Red should wrap the string in ANSI codes")
	}
	if got[0] != '\033' || got[len(got)-1] != 'm' {
		t.Errorf("expected ANSI-wrapped string, got %q", got)
	}
}
