package agent

import (
	"errors"
	"strings"
)

const (
	outputLoopMinRepeats       = 3
	outputLoopMinParagraphSize = 80
	outputLoopMaxPendingSize   = 64 * 1024
)

var errAssistantOutputLoop = errors.New(
	"assistant output loop detected: identical substantive paragraph repeated 3 times; stopping run",
)

// outputLoopDetector recognizes the provider failure where one substantive
// assistant paragraph is emitted forever. It operates on completed paragraphs
// rather than provider deltas, whose boundaries are not semantically stable.
// Only consecutive matches count, and short paragraphs cannot trigger it.
type outputLoopDetector struct {
	pending     string
	last        string
	repetitions int
}

func newOutputLoopDetector() *outputLoopDetector {
	return &outputLoopDetector{}
}

// Observe adds streamed assistant text and reports whether the run must stop.
func (d *outputLoopDetector) Observe(delta string) bool {
	d.pending += delta
	for {
		end := strings.Index(d.pending, "\n\n")
		if end < 0 {
			break
		}
		paragraph := d.pending[:end]
		d.pending = d.pending[end+2:]
		if d.observeParagraph(paragraph) {
			return true
		}
	}
	if len(d.pending) > outputLoopMaxPendingSize {
		// A paragraph with no terminator must not grow this per-turn buffer
		// without bound. Retain a suffix so a later terminator still completes
		// it, but reset the sequence because its beginning is no longer known.
		d.pending = d.pending[len(d.pending)-outputLoopMaxPendingSize/2:]
		d.last = ""
		d.repetitions = 0
	}
	return false
}

func (d *outputLoopDetector) observeParagraph(paragraph string) bool {
	normalized := strings.Join(strings.Fields(paragraph), " ")
	if len(normalized) < outputLoopMinParagraphSize || len(strings.Fields(normalized)) < 12 {
		return false
	}
	if normalized == d.last {
		d.repetitions++
	} else {
		d.last = normalized
		d.repetitions = 1
	}
	return d.repetitions >= outputLoopMinRepeats
}
