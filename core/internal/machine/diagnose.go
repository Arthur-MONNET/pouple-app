package machine

import (
	"fmt"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// diskFullSigns are how a process says the disk is full, whatever program wrote it.
var diskFullSigns = []string{"no space left on device", "enospc", "disk is full", "disk full"}

// Diagnose says, in one or two sentences, whether the disk or the memory explains a failure: the AIs fail with their
// own errors when the Mac runs out of either, and never name the real cause. cause is the failure's text; state is
// read when it happened. It returns "" when nothing of the machine could be read.
func Diagnose(lang i18n.Lang, cause string, state State) string {
	// Resource pressure alone cannot explain a remote refusal or an application error.
	lower := strings.ToLower(cause)
	compatible := mentionsFullDisk(cause)
	for _, sign := range []string{"out of memory", "cannot allocate memory", "enomem", "resource temporarily unavailable", "exit status 137", "signal: killed"} {
		compatible = compatible || strings.Contains(lower, sign)
	}
	for _, sign := range []string{"http 401", "http 403", "answered 401", "answered 403", "unauthorized", "forbidden", "access refused", "authentication", "scope", "project access"} {
		if strings.Contains(lower, sign) {
			return ""
		}
	}
	if !compatible {
		return ""
	}
	free, used := "?", "?"
	if state.Disk != nil {
		free = Bytes(lang, state.Disk.FreeBytes)
	}
	if state.Memory != nil {
		used = Percent(lang, state.Memory.UsedPercent)
	}
	if mentionsFullDisk(cause) {
		if state.Disk == nil {
			return i18n.T(lang, "The disk is full: that is why it failed. Free some space, then investigate again.")
		}
		return i18n.T(lang, "The disk is full (%s free): that is why it failed. Free some space, then investigate again.", free)
	}
	if state.Disk == nil && state.Memory == nil {
		return ""
	}
	diskShort := state.Disk != nil && state.Disk.Level == Critical
	memoryShort := state.Memory != nil && state.Memory.Level == Critical
	switch {
	case diskShort && memoryShort:
		return i18n.T(lang, "When it failed, the disk was almost full (%s free) and the Mac was short of memory (%s in use): that is most likely why. Free some space and close some apps, then investigate again.", free, used)
	case diskShort:
		return i18n.T(lang, "When it failed, the disk was almost full (%s free): that is most likely why. Free some space, then investigate again.", free)
	case memoryShort:
		return i18n.T(lang, "When it failed, the Mac was short of memory (%s in use): that is most likely why. Close some apps, then investigate again.", used)
	}
	if (state.Disk != nil && state.Disk.Level == High) || (state.Memory != nil && state.Memory.Level == High) {
		return i18n.T(lang, "When it failed, the disk had %s free and the memory was %s in use: tight, so it may have played a part.", free, used)
	}
	return i18n.T(lang, "When it failed, the disk had %s free and the memory was %s in use: neither is the cause.", free, used)
}

func mentionsFullDisk(cause string) bool {
	lower := strings.ToLower(cause)
	for _, sign := range diskFullSigns {
		if strings.Contains(lower, sign) {
			return true
		}
	}
	return false
}

// Bytes is a size in gigabytes as each language writes it: "2,4 Go", "2.4 GB"; without decimals from 100.
func Bytes(lang i18n.Lang, value int64) string {
	gigabytes := float64(value) / float64(gib)
	format := "%.1f"
	if gigabytes >= 100 {
		format = "%.0f"
	}
	number := fmt.Sprintf(format, gigabytes)
	if lang == i18n.EN {
		return number + " GB"
	}
	return strings.Replace(number, ".", ",", 1) + " Go"
}

// Percent is a share from 0 to 100 as each language writes it: "61 %", "61%".
func Percent(lang i18n.Lang, value float64) string {
	if lang == i18n.EN {
		return fmt.Sprintf("%.0f%%", value)
	}
	return fmt.Sprintf("%.0f %%", value)
}
