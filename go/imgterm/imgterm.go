package imgterm

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/mattn/go-colorable"
	isatty "github.com/mattn/go-isatty"
	sixel "github.com/mattn/go-sixel"
	"github.com/mattn/longcat/ascii"
	"github.com/mattn/longcat/extraterm"
	"github.com/mattn/longcat/iterm"
	"github.com/mattn/longcat/kitty"
	"github.com/mattn/longcat/pixterm"
	"golang.org/x/term"
)

func getDA2() string {
	s, err := term.MakeRaw(1)
	if err != nil {
		return ""
	}
	defer term.Restore(1, s)
	_, err = os.Stdout.Write([]byte("\x1b[>c")) // DA2 host request
	if err != nil {
		return ""
	}
	defer os.Stdout.SetReadDeadline(time.Time{})

	time.Sleep(10 * time.Millisecond)

	var b [100]byte
	n, err := os.Stdout.Read(b[:])
	if err != nil {
		return ""
	}
	return string(b[:n])
}

func checkIterm() bool {
	if strings.HasPrefix(os.Getenv("TERM_PROGRAM"), "iTerm") {
		return true
	}
	return getDA2() == "\x1b[>0;95;0c" // iTerm2 version 3
}

func checkKitty() bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if os.Getenv("TERM_PROGRAM") == "ghostty" {
		return true
	}
	return strings.HasPrefix(getDA2(), "\x1b[>1;4000;") // \x1b[>1;{major+4000};{minor}c
}

func checkSixel() bool {
	if isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		return true
	}
	s, err := term.MakeRaw(1)
	if err == nil {
		defer term.Restore(1, s)
	}
	_, err = os.Stdout.Write([]byte("\x1b[c"))
	if err != nil {
		return false
	}
	defer os.Stdout.SetReadDeadline(time.Time{})

	time.Sleep(10 * time.Millisecond)

	var b [100]byte
	n, err := os.Stdout.Read(b[:])
	if err != nil {
		return false
	}

	if bytes.HasPrefix(b[:n], []byte("\x1b[?1;2;4c")) {
		return true
	}
	var supportedTerminals = []string{
		"\x1b[?62;", // VT240
		"\x1b[?63;", // wsltty
		"\x1b[?64;", // mintty
		"\x1b[?65;", // RLogin
	}
	supported := false
	for _, supportedTerminal := range supportedTerminals {
		if bytes.HasPrefix(b[:n], []byte(supportedTerminal)) {
			supported = true
			break
		}
	}
	if !supported {
		return false
	}

	sb := b[6:n]
	n = bytes.IndexByte(sb, 'c')
	if n != -1 {
		sb = sb[:n]
	}
	for _, t := range bytes.Split(sb, []byte(";")) {
		if len(t) == 1 && t[0] == '4' {
			return true
		}
	}
	return false
}

func checkExtraterm() bool {
	return os.Getenv("LC_EXTRATERM_COOKIE") != ""
}

func check8BitColor() bool {
	if os.Getenv("TERM_PROGRAM") == "Apple_Terminal" { // Terminal.app
		return true
	}
	da2 := getDA2()
	var supportedTerminals = []string{
		"\x1b[>1;95;0c",  // Terminal.app
		"\x1b[>0;276;0c", // tty.js (xterm mode)
	}
	for _, supportedTerminal := range supportedTerminals {
		if da2 == supportedTerminal {
			return true
		}
	}
	return false
}

func Display(img image.Image) (error, string) {
	var buf bytes.Buffer
	var enc interface {
		Encode(image.Image) error
	}

	isPixterm := false
	asciiMode := false
	vtenabled := false
	is8BitColor := false
	encoder := "???"

	defer colorable.EnableColorsStdout(&vtenabled)()

	if runtime.GOOS == "windows" && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		if os.Getenv("LONGCAT_WINDOWS_USE_SIXEL") == "1" {
			enc = sixel.NewEncoder(&buf)
			encoder = "sixel"
		} else if vtenabled {
			isPixterm = true
		} else {
			asciiMode = true
		}
	} else if checkIterm() {
		enc = iterm.NewEncoder(&buf)
		encoder = "iterm"
	} else if checkKitty() {
		enc = kitty.NewEncoder(&buf)
		encoder = "kitty"
	} else if checkSixel() {
		enc = sixel.NewEncoder(&buf)
		encoder = "sixel"
	} else if checkExtraterm() {
		enc = extraterm.NewEncoder(&buf)
		encoder = "extraterm"
	} else {
		isPixterm = true
	}

	if isPixterm {
		is8BitColor = is8BitColor || check8BitColor()
		enc = pixterm.NewEncoder(&buf, is8BitColor)
		encoder = "pixterm"
	}

	if asciiMode {
		enc = ascii.NewEncoder(&buf)
		encoder = "ascii"
	}

	if err := enc.Encode(img); err != nil {
		return fmt.Errorf("failed to encode image using %s: %w", encoder, err), encoder
	}

	if runtime.GOOS == "windows" {
		colorable.NewColorableStdout().Write(buf.Bytes())
	} else {
		os.Stdout.Write(buf.Bytes())
	}
	os.Stdout.Sync()

	return nil, encoder
}
