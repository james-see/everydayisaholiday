package timezone

import "time"

// Valid reports whether name is a known IANA timezone.
func Valid(name string) bool {
	if name == "" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}
