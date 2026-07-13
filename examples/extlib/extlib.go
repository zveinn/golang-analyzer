// Package extlib simulates an external dependency so the analyzer's
// [module] classification can be tested without network access.
package extlib

import "fmt"

func Validate(name string) error {
	if name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func Transform(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ 0x5a
	}
	return out
}
