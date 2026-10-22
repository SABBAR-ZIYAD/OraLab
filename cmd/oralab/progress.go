package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type cliProgress struct {
	writer          io.Writer
	now             func() time.Time
	lastImageUpdate time.Time
}

func newCLIProgress(writer io.Writer) *cliProgress {
	return &cliProgress{writer: writer, now: time.Now}
}

func (p *cliProgress) heading() {
	if p == nil || p.writer == nil {
		return
	}
	fmt.Fprintln(p.writer, "OraLab - Oracle Database 19c")
	fmt.Fprintln(p.writer)
}

func (p *cliProgress) waiting(format string, arguments ...any) {
	p.write("[..]", format, arguments...)
}

func (p *cliProgress) success(format string, arguments ...any) {
	p.write("[OK]", format, arguments...)
}

func (p *cliProgress) failed(format string, arguments ...any) {
	p.write("[FAIL]", format, arguments...)
}

func (p *cliProgress) write(prefix, format string, arguments ...any) {
	if p == nil || p.writer == nil {
		return
	}
	fmt.Fprintf(p.writer, "%s %s\n", prefix, fmt.Sprintf(format, arguments...))
}

func (p *cliProgress) imagePullUpdate(update engineImageProgress) {
	if p == nil || p.writer == nil {
		return
	}
	now := p.now()
	if !p.lastImageUpdate.IsZero() && now.Sub(p.lastImageUpdate) < 5*time.Second {
		return
	}
	status := strings.TrimSpace(update.Status)
	if status == "" {
		return
	}
	layer := strings.TrimSpace(update.ID)
	if len(layer) > 12 {
		layer = layer[:12]
	}
	detail := status
	if layer != "" {
		detail += " [" + layer + "]"
	}
	if update.Total > 0 && update.Current >= 0 {
		percentage := update.Current * 100 / update.Total
		if percentage > 100 {
			percentage = 100
		}
		detail += fmt.Sprintf(" (%d%%)", percentage)
	}
	p.waiting("Pulling Oracle image: %s", detail)
	p.lastImageUpdate = now
}

func (p *cliProgress) readinessUpdate(update readinessProgress) {
	status := strings.TrimSpace(update.Status)
	if status == "" {
		status = "starting"
	}
	p.waiting("Oracle is %s (elapsed %s)", status, formatElapsed(update.Elapsed))
}

func formatElapsed(elapsed time.Duration) string {
	elapsed = elapsed.Round(time.Second)
	if elapsed < time.Second {
		return "0s"
	}
	return elapsed.String()
}
