package main

import (
	"bufio"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportDirResolvesRelativeOverrides(t *testing.T) {
	t.Setenv("POUPLE_HOME", "pouple-data")
	want, err := filepath.Abs("pouple-data")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := supportDir(); got != want || err != nil {
		t.Fatalf("supportDir = %q, %v; want %q", got, err, want)
	}
}

func TestReadShellToken(t *testing.T) {
	token := strings.Repeat("ab", 32)
	input := bufio.NewReaderSize(strings.NewReader(token+"\nremaining\n"), 65)
	if got, err := readShellToken(input); got != token || err != nil {
		t.Fatalf("valid startup line rejected: %v", err)
	}
	if rest, err := input.ReadString('\n'); rest != "remaining\n" || err != nil {
		t.Fatalf("lifetime pipe lost buffered data: %q, %v", rest, err)
	}
	for _, bad := range []string{token, "short\n", strings.Repeat("z", 64) + "\n", strings.Repeat("a", 1024) + "\n"} {
		if _, err := readShellToken(bufio.NewReaderSize(strings.NewReader(bad), 65)); err == nil {
			t.Error("invalid startup line accepted")
		}
	}
}
