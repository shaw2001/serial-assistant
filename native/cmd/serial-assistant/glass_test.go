package main

import "testing"

func TestGlassBackdropSupported(t *testing.T) {
	if glassBackdropSupported(19045) {
		t.Fatal("Windows 10 should use the opaque fallback")
	}
	if glassBackdropSupported(22000) != true {
		t.Fatal("Windows 11 should attempt the DWM backdrop")
	}
	if glassBackdropSupported(22621) != true {
		t.Fatal("Windows 11 22H2 should attempt the DWM backdrop")
	}
}
