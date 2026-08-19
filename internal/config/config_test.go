package config

import "testing"

func TestParseMemLimit(t *testing.T) {
	n, err := ParseMemLimit("384MiB")
	if err != nil || n != 384<<20 {
		t.Fatalf("%d %v", n, err)
	}
	n, err = ParseMemLimit("1GiB")
	if err != nil || n != 1<<30 {
		t.Fatalf("%d %v", n, err)
	}
	n, err = ParseMemLimit("")
	if err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
}
