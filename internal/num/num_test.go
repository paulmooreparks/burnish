package num

import "testing"

func TestRound(t *testing.T) {
	cases := []struct {
		f      float64
		places int
		want   float64
	}{
		{0.12345, 4, 0.1235},
		{1.0, 4, 1.0},
		{-2.5, 0, -3}, // half away from zero, unlike a truncating round
		{0.9006, 3, 0.901},
	}
	for _, c := range cases {
		if got := Round(c.f, c.places); got != c.want {
			t.Errorf("Round(%v,%d) = %v, want %v", c.f, c.places, got, c.want)
		}
	}
}
