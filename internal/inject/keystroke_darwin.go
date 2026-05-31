//go:build darwin

package inject

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// vp_type posts a Unicode string as a keyboard event to the focused app. We use
// CGEventKeyboardSetUnicodeString so arbitrary characters work regardless of the
// active keyboard layout.
static void vp_type(const UniChar *buf, int len) {
    CGEventRef down = CGEventCreateKeyboardEvent(NULL, 0, true);
    CGEventKeyboardSetUnicodeString(down, len, buf);
    CGEventPost(kCGHIDEventTap, down);
    CFRelease(down);

    CGEventRef up = CGEventCreateKeyboardEvent(NULL, 0, false);
    CGEventKeyboardSetUnicodeString(up, len, buf);
    CGEventPost(kCGHIDEventTap, up);
    CFRelease(up);
}

// vp_key presses and releases a virtual key code (36 = Return).
static void vp_key(CGKeyCode code) {
    CGEventRef down = CGEventCreateKeyboardEvent(NULL, code, true);
    CGEventPost(kCGHIDEventTap, down);
    CFRelease(down);

    CGEventRef up = CGEventCreateKeyboardEvent(NULL, code, false);
    CGEventPost(kCGHIDEventTap, up);
    CFRelease(up);
}

// vp_paste presses Cmd+V (V = key code 9) to paste the clipboard.
static void vp_paste(void) {
    CGEventRef down = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)9, true);
    CGEventSetFlags(down, kCGEventFlagMaskCommand);
    CGEventPost(kCGHIDEventTap, down);
    CFRelease(down);

    CGEventRef up = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)9, false);
    CGEventSetFlags(up, kCGEventFlagMaskCommand);
    CGEventPost(kCGHIDEventTap, up);
    CFRelease(up);
}
*/
import "C"

import (
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	vkReturn = 36

	// maxUnitsPerEvent batches several characters into a single keyboard event.
	// Posting one event per character floods the system and macOS drops all but
	// the first; batching keeps event volume low. Kept conservative because very
	// long Unicode strings per event are unreliable on older macOS.
	maxUnitsPerEvent = 16

	// interEventDelay gives the focused app's event loop time to drain each event
	// so none are coalesced or dropped.
	interEventDelay = 4 * time.Millisecond

	// settleDelay lets the last events flush before the process exits.
	settleDelay = 20 * time.Millisecond
)

// emitText types text into the focused window, optionally pressing Return.
//
// Requires Accessibility permission: System Settings → Privacy & Security →
// Accessibility. Until granted, posted events are silently dropped by macOS.
func emitText(text string, submit bool) error {
	// Build UTF-16 chunks on rune boundaries (so surrogate pairs/emoji are never
	// split) up to maxUnitsPerEvent, posting one event per chunk with a delay.
	var buf []uint16
	flush := func() {
		if len(buf) == 0 {
			return
		}
		C.vp_type((*C.UniChar)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
		buf = buf[:0]
		time.Sleep(interEventDelay)
	}
	for _, r := range text {
		u := utf16.Encode([]rune{r})
		if len(buf)+len(u) > maxUnitsPerEvent {
			flush()
		}
		buf = append(buf, u...)
	}
	flush()

	if submit {
		C.vp_key(C.CGKeyCode(vkReturn))
	}
	time.Sleep(settleDelay)
	return nil
}

// emitPaste presses Cmd+V (used by the clipboard sink's auto-paste), optionally
// pressing Return afterwards. Requires the same Accessibility permission as
// emitText.
func emitPaste(submit bool) error {
	C.vp_paste()
	if submit {
		C.vp_key(C.CGKeyCode(vkReturn))
	}
	return nil
}
