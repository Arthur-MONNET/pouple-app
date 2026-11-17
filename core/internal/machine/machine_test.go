package machine

import (
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

func TestDiskLevelFollowsFreeSpaceNotShare(t *testing.T) {
	cases := map[int64]Level{gib / 2: Critical, 2*gib - 1: Critical, 2 * gib: High, 10*gib - 1: High, 10 * gib: Normal, 300 * gib: Normal}
	for free, want := range cases {
		if got := newDisk(free, 500*gib).Level; got != want {
			t.Errorf("free %d: level %s, want %s", free, got, want)
		}
	}
	if used := newDisk(2*gib, 500*gib).UsedPercent(); used < 99.5 || used > 99.7 {
		t.Errorf("used share %.2f, want about 99.6", used)
	}
	if used := (Disk{}).UsedPercent(); used != 0 {
		t.Errorf("an unknown size is %v%% used, want 0", used)
	}
}

func TestMemoryLevelIsTheSystemPressure(t *testing.T) {
	cases := []struct {
		pressure uint32
		want     Level
	}{{1, Normal}, {2, High}, {4, Critical}}
	for _, c := range cases {
		memory := newMemory(39, c.pressure, 24*gib)
		if memory.Level != c.want || memory.UsedPercent != 61 {
			t.Errorf("pressure %d: %+v, want level %s and 61%% in use", c.pressure, memory, c.want)
		}
	}
}

func TestMonitorReportsOnlyWhatTheScreensShow(t *testing.T) {
	free := 50 * gib
	pressure := uint32(1)
	monitor := newMonitor("/data", func(string) State {
		return State{Disk: newDisk(free, 500*gib), Memory: newMemory(60, pressure, 24*gib)}
	})
	if monitor.Current().Disk.FreeBytes != 50*gib {
		t.Fatal("the monitor must read the machine when it is made")
	}
	free += 1 << 20 // A megabyte is below what the screens show.
	if monitor.Refresh() {
		t.Error("a change the screens do not show was reported")
	}
	free -= gib
	if !monitor.Refresh() {
		t.Error("a gigabyte less was not reported")
	}
	pressure = 4
	if !monitor.Refresh() || monitor.Current().Memory.Level != Critical {
		t.Error("the memory pressure becoming critical was not reported")
	}
}

func TestDiagnoseNamesTheMachineOnlyWhenItIsShort(t *testing.T) {
	fine := State{Disk: newDisk(200*gib, 500*gib), Memory: newMemory(60, 1, 24*gib)}
	fullDisk := State{Disk: newDisk(gib, 500*gib), Memory: newMemory(60, 1, 24*gib)}
	noMemory := State{Disk: newDisk(200*gib, 500*gib), Memory: newMemory(5, 4, 24*gib)}
	tight := State{Disk: newDisk(5*gib, 500*gib), Memory: newMemory(60, 1, 24*gib)}
	both := State{Disk: newDisk(gib, 500*gib), Memory: newMemory(5, 4, 24*gib)}
	cases := []struct {
		name, cause string
		state       State
		want        string
	}{
		{"the error says the disk is full", "write rollout: No space left on device", fine, "The disk is full (200 GB free): that is why it failed."},
		{"the error says it and nothing was read", "ENOSPC", State{}, "The disk is full: that is why it failed."},
		{"almost no space left", "thread/resume failed", fullDisk, "the disk was almost full (1.0 GB free): that is most likely why."},
		{"out of memory", "exit status 1", noMemory, "the Mac was short of memory (95% in use): that is most likely why."},
		{"both short", "exit status 1", both, "the disk was almost full (1.0 GB free) and the Mac was short of memory (95% in use)"},
		{"tight", "exit status 1", tight, "the disk had 5.0 GB free and the memory was 40% in use: tight, so it may have played a part."},
		{"fine", "exit status 1", fine, "the disk had 200 GB free and the memory was 40% in use: neither is the cause."},
		{"half read", "exit status 1", State{Memory: newMemory(60, 1, 24*gib)}, "the disk had ? free"},
	}
	for _, c := range cases {
		if got := Diagnose(i18n.EN, c.cause, c.state); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want it to contain %q", c.name, got, c.want)
		}
	}
	if got := Diagnose(i18n.EN, "exit status 1", State{}); got != "" {
		t.Errorf("nothing read must say nothing, got %q", got)
	}
	if got := Diagnose(i18n.FR, "exit status 1", fullDisk); !strings.Contains(got, "presque plein (1,0 Go libres)") {
		t.Errorf("French: %q", got)
	}
}

func TestSizesAndSharesAreWrittenInEachLanguage(t *testing.T) {
	if got := Bytes(i18n.FR, 2*gib+gib/2); got != "2,5 Go" {
		t.Errorf("French size %q", got)
	}
	if got := Bytes(i18n.EN, 212*gib); got != "212 GB" {
		t.Errorf("English size %q", got)
	}
	if got := Percent(i18n.FR, 61.4); got != "61 %" {
		t.Errorf("French share %q", got)
	}
}
