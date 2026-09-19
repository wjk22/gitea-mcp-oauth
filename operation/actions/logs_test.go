package actions

import "testing"

func TestTailByLines(t *testing.T) {
	in := []byte("a\nb\nc\nd\n")
	got := string(tailByLines(in, 2))
	if got != "c\nd\n" {
		t.Fatalf("tailByLines(...,2) = %q", got)
	}
}

func TestLimitBytesKeepsTail(t *testing.T) {
	in := []byte("0123456789")
	out, truncated := limitBytes(in, 4)
	if !truncated {
		t.Fatalf("expected truncated=true")
	}
	if string(out) != "6789" {
		t.Fatalf("limitBytes tail = %q, want %q", string(out), "6789")
	}
}
