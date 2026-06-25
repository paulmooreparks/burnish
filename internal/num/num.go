// Package num holds small numeric helpers shared across the engine so there is a
// single implementation rather than per-package copies that can drift.
package num

import "math"

// Round rounds f to the given number of decimal places (half away from zero, via
// math.Round). Used to keep profile YAML stable and diffable.
func Round(f float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(f*p) / p
}
