package ui

import (
	"testing"
)

func TestGetInitials(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Two words", "John Doe", "JD"},
		{"Single word", "Alice", "AL"},
		{"Three words", "John William Doe", "JW"},
		{"Lowercase", "john doe", "JD"},
		{"Empty", "", ""},
	}

	for _, tc := range tests {
		result := getInitials(tc.input)
		if result != tc.expected {
			t.Errorf("getInitials(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestGetAvatarColor(t *testing.T) {
	color1 := getAvatarColor("Alice")
	color2 := getAvatarColor("Bob")

	if color1 == color2 {
		t.Logf("Colors same for different names (may happen by chance)")
	}

	for i := 0; i < 10; i++ {
		name := string(rune('A' + i))
		color := getAvatarColor(name)
		if color.A != 255 {
			t.Errorf("Avatar color alpha should be 255, got %d", color.A)
		}
	}
}

func TestBrighten(t *testing.T) {
	if brighten(100, 50) != 150 {
		t.Errorf("brighten(100, 50) should be 150")
	}

	if brighten(250, 50) != 255 {
		t.Errorf("brighten(250, 50) should saturate at 255")
	}

	if brighten(100, -50) != 50 {
		t.Errorf("brighten(100, -50) should be 50")
	}
}
