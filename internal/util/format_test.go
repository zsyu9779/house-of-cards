package util

import "testing"

func TestOrDash(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "-"},
		{"hello", "hello"},
		{" ", " "},
	}
	for _, c := range cases {
		if got := OrDash(c.in); got != c.want {
			t.Errorf("OrDash(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"shorter than max", "abc", 10, "abc"},
		{"equal to max", "abcdef", 6, "abcdef"},
		{"longer than max", "abcdefghij", 6, "abc..."},
		{"multibyte runes preserved", "你好世界啊啊啊", 5, "你好..."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Truncate(c.s, c.max); got != c.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
			}
		})
	}
}
