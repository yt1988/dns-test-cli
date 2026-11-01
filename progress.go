package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// The progress display in a color terminal (from vps2cloud): a little Shiba Inu, drawn with half blocks (two pixels per
// character), runs along a track as the test goes, its legs moving on a timer, with the percentage and what is being
// asked under it.

// Two frames, six pixel rows each (three lines of text). O coat, C cream, K eye and nose, . nothing.
var dogFrames = [2][6]string{
	{
		"...........O..O.",
		".OO........OOOOO",
		"O..OOOOOOOOOKOOC",
		".OOOOOOOOOOOCCCK",
		"..OOCCCCCCOOC...",
		"..O..O....O..O..",
	},
	{
		"...........O..O.",
		".OO........OOOOO",
		"O..OOOOOOOOOKOOC",
		".OOOOOOOOOOOCCCK",
		"..OOCCCCCCOOC...",
		"...OO......OO...",
	},
}

const dogWidth = 16

// Shiba colors: 24-bit where the terminal says it has them, the nearest of the 256 otherwise
var dogColors = map[byte][2]string{
	'O': {"38;2;226;128;58", "38;5;173"},
	'C': {"38;2;247;228;196", "38;5;223"},
	'K': {"38;2;40;32;28", "38;5;235"},
}

func dogColor(p byte, bg bool) string {
	c := dogColors[p][1]
	if t := os.Getenv("COLORTERM"); t == "truecolor" || t == "24bit" {
		c = dogColors[p][0]
	}
	if bg {
		c = "4" + c[1:] // 38;… → 48;…
	}
	return "\x1b[" + c + "m"
}

// dogLine draws two pixel rows as one line of half blocks.
func dogLine(top, bottom string) string {
	var b strings.Builder
	for i := 0; i < len(top); i++ {
		t, u := top[i], bottom[i]
		switch {
		case t == '.' && u == '.':
			b.WriteByte(' ')
		case u == '.':
			b.WriteString(dogColor(t, false) + "▀" + cReset)
		case t == '.':
			b.WriteString(dogColor(u, false) + "▄" + cReset)
		default:
			b.WriteString(dogColor(t, false) + dogColor(u, true) + "▀" + cReset)
		}
	}
	return b.String()
}

type runner struct {
	mu            sync.Mutex
	out           io.Writer
	track         int
	done, total   int
	text          string
	frame         int
	drawn         bool
	stop, stopped chan struct{}
}

// startRunner begins the animation in a terminal cols wide; nil when it is too narrow for the dog to run.
func startRunner(out io.Writer, cols int) *runner {
	track := cols - dogWidth - 2
	if track < 8 {
		return nil
	}
	if track > 40 {
		track = 40
	}
	r := &runner{out: out, track: track, stop: make(chan struct{}), stopped: make(chan struct{})}
	fmt.Fprint(out, "\x1b[?25l") // no cursor blinking over the dog
	go func() {
		defer close(r.stopped)
		t := time.NewTicker(120 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-t.C:
				r.mu.Lock()
				r.frame++
				r.draw()
				r.mu.Unlock()
			}
		}
	}()
	return r
}

func (r *runner) update(done, total int, text string) {
	r.mu.Lock()
	r.done, r.total, r.text = done, total, text
	r.draw()
	r.mu.Unlock()
}

// draw writes the three dog lines and the text line, going back over the previous drawing first.
func (r *runner) draw() {
	var b strings.Builder
	if r.drawn {
		b.WriteString("\x1b[3A")
	}
	x := 0
	if r.total > 0 {
		x = r.done * r.track / r.total
	}
	pad := strings.Repeat(" ", x)
	f := dogFrames[r.frame%2]
	for i := 0; i < 6; i += 2 {
		b.WriteString("\r\x1b[K" + pad + dogLine(f[i], f[i+1]) + "\n")
	}
	b.WriteString("\r\x1b[K" + r.text)
	fmt.Fprint(r.out, b.String())
	r.drawn = true
}

// println prints a finished region's line where the dog is and draws the dog again under it.
func (r *runner) println(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drawn {
		fmt.Fprint(r.out, "\x1b[3A\r\x1b[J")
	}
	fmt.Fprint(r.out, line+"\n")
	r.drawn = false
	r.draw()
}

// finish stops the animation and clears it, leaving the cursor where the dog started.
func (r *runner) finish() {
	close(r.stop)
	<-r.stopped
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drawn {
		fmt.Fprint(r.out, "\x1b[3A\r\x1b[J")
	}
	fmt.Fprint(r.out, "\x1b[?25h")
}
