// Package screenshot opens a page in a Chromium browser of the Mac and returns a picture of it. The AI cannot do it
// itself (its sandbox runs no browser and no local server): it names a page, Pouple takes the picture.
//
// A page is opened only if it is one the user's own computer serves (localhost) or a site the user listed, with a
// browser profile that is empty and thrown away afterwards, so no session, cookie or extension is ever involved.
package screenshot

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	timeout = 45 * time.Second
	// maxBytes bounds a picture: it is shown to the user and handed to the AI.
	maxBytes = 8 << 20
)

// Viewports are the sizes the AI may ask for, by name.
var viewports = map[string]size{
	"":        {1280, 900, false},
	"desktop": {1280, 900, false},
	"mobile":  {390, 844, true},
}

// mobileAgent is what a phone says it is, so a site that serves another page to phones serves it.
const mobileAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"

// Taker takes pictures of pages.
type Taker struct {
	// Hosts returns the sites, besides localhost, that may be opened; read at each picture so a change applies at once.
	Hosts func() []string
	// Forbidden are local ports that are never opened: Pouple's own.
	Forbidden func() []int
	// Browsers are the Chromium-based executables to try, in order; the usual places on a Mac when nil.
	Browsers []string
}

var defaultBrowsers = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
}

func (t *Taker) browser() (string, error) {
	candidates := t.Browsers
	if candidates == nil {
		candidates = defaultBrowsers
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o100 != 0 {
			return path, nil
		}
	}
	return "", i18n.NewError("No Chromium-based browser (Chrome, Chromium, Edge or Brave) was found on this Mac")
}

// Check says whether a page may be opened, and returns its clean address.
func (t *Taker) Check(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", i18n.NewError("A screenshot needs a web address starting with http:// or https://")
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if !local && !t.hostAllowed(host) {
		return "", i18n.NewError("Pouple only opens localhost and the sites you listed for screenshots: %s is not one of them", host)
	}
	if local && t.Forbidden != nil {
		if number, err := strconv.Atoi(port); err == nil {
			for _, forbidden := range t.Forbidden() {
				if number == forbidden {
					return "", i18n.NewError("This address is Pouple's own and is never opened")
				}
			}
		}
	}
	// A fragment never reaches the server, and the picture does not need it.
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (t *Taker) hostAllowed(host string) bool {
	if t.Hosts == nil {
		return false
	}
	for _, allowed := range t.Hosts() {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return true
		}
	}
	return false
}

// Observer follows a picture being taken, so the user can watch the browser work. Every field may be nil.
type Observer struct {
	// Step says what the browser is doing, in a few words ("Opening the page", "Waiting for the page to settle").
	Step func(text string)
	// Frame receives a picture (JPEG) of what the browser shows, as often as it redraws.
	Frame func(jpeg []byte)
	// Foreign is called when the page reaches beyond this computer (a listed site, a CDN): what comes back is written by
	// others.
	Foreign func()
}

func (o *Observer) foreign() {
	if o != nil && o.Foreign != nil {
		o.Foreign()
	}
}

func (o *Observer) step(text string) {
	if o != nil && o.Step != nil {
		o.Step(text)
	}
}

func (o *Observer) frame(jpeg []byte) {
	if o != nil && o.Frame != nil {
		o.Frame(jpeg)
	}
}

// Take opens the page and returns a PNG of what it shows. viewport is "", "desktop" or "mobile"; observer may be nil.
func (t *Taker) Take(ctx context.Context, address, viewport string, observer *Observer) ([]byte, error) {
	clean, err := t.Check(address)
	if err != nil {
		return nil, err
	}
	size, ok := viewports[strings.ToLower(strings.TrimSpace(viewport))]
	if !ok {
		return nil, i18n.NewError("The screen size must be \"desktop\" or \"mobile\"")
	}
	browser, err := t.browser()
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "pouple-shot-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	observer.step("Starting the browser")
	conn, stop, err := launch(runCtx, browser, work)
	if err != nil {
		return nil, err
	}
	defer stop()
	data, err := capture(runCtx, conn, clean, size, observer, t.allow)
	switch {
	case err == nil:
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return nil, i18n.NewError("The page took too long to open")
	case ctx.Err() != nil:
		return nil, ctx.Err()
	default:
		return nil, i18n.Wrap(err, "The browser could not open the page: %s", err)
	}
	if len(data) == 0 {
		return nil, i18n.NewError("The browser did not produce a picture of the page")
	}
	if len(data) > maxBytes {
		return nil, i18n.NewError("The picture is too large")
	}
	return data, nil
}

// launch starts the browser with an empty profile and talks to it over a pipe that only Pouple holds: the browser
// listens on no port. stop ends it with everything it started.
func launch(ctx context.Context, browser, work string) (*connection, func(), error) {
	toBrowserRead, toBrowserWrite, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	fromBrowserRead, fromBrowserWrite, err := os.Pipe()
	if err != nil {
		toBrowserRead.Close()
		toBrowserWrite.Close()
		return nil, nil, err
	}
	command := exec.Command(browser,
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--disable-background-networking", "--disable-sync", "--mute-audio",
		// Without it Chrome asks the macOS keychain for its storage key at every start: a prompt for the user, and ten
		// seconds of waiting.
		"--use-mock-keychain", "--password-store=basic",
		"--remote-debugging-pipe", "--user-data-dir="+filepath.Join(work, "profile"), "about:blank")
	command.Env = []string{"HOME=" + work, "PATH=/usr/bin:/bin", "TMPDIR=" + work}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The browser reads commands on its descriptor 3 and writes its answers on 4.
	command.ExtraFiles = []*os.File{toBrowserRead, fromBrowserWrite}
	err = command.Start()
	toBrowserRead.Close()
	fromBrowserWrite.Close()
	if err != nil {
		toBrowserWrite.Close()
		fromBrowserRead.Close()
		return nil, nil, i18n.Wrap(err, "The browser could not be started")
	}
	stop := func() {
		// The browser starts helpers of its own: the whole group goes.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
		toBrowserWrite.Close()
		fromBrowserRead.Close()
	}
	return newConnection(fromBrowserRead, toBrowserWrite), stop, nil
}

// passive are the kinds of request that only bring something to draw: they may come from any site, while everything that
// can navigate or send data (pages, frames, scripts' requests, sockets) must go where the user allowed.
var passive = map[string]bool{"Stylesheet": true, "Image": true, "Media": true, "Font": true, "Script": true, "TextTrack": true, "Manifest": true}

// allow decides whether the page may make a request. Every request is looked at, redirects and frames included, so a
// page cannot take the browser somewhere the first address did not.
func (t *Taker) allow(rawURL, resourceType string) (allow, foreign bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false, false
	}
	switch parsed.Scheme {
	case "data", "blob", "about":
		return true, false
	case "http", "https":
	default:
		return false, false
	}
	host := strings.ToLower(parsed.Hostname())
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if local {
		_, err := t.Check(rawURL)
		return err == nil, false
	}
	if passive[resourceType] || t.hostAllowed(host) {
		return true, true
	}
	return false, false
}

// PortOf returns the port of a listener address such as "127.0.0.1:8123", for Forbidden.
func PortOf(address string) int {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return 0
	}
	number, _ := strconv.Atoi(port)
	return number
}
