package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// This file holds the layout-measurement helpers shared by the terminal and
// HTML renderers. They translate the declarative sizing vocabulary of UIProps
// and UIArea into concrete numbers. Keeping them here means both renderers
// resolve "fill", "auto", "40", and "25%" identically; only the unit they apply
// the result in (terminal cells vs CSS) differs.

import (
	"fmt"
	"strings"
)

// PropSides expands a 1/2/4-length padding or margin spec into
// top,right,bottom,left. ok is false for any other length.
func PropSides(v []int) (top, right, bottom, left int, ok bool) {
	switch len(v) {
	case 1:
		return v[0], v[0], v[0], v[0], true
	case 2:
		return v[0], v[1], v[0], v[1], true
	case 4:
		return v[0], v[1], v[2], v[3], true
	}
	return 0, 0, 0, 0, false
}

// ParseDimension resolves a UIProps Width/Height value. "fill" resolves to
// fillWidth; "auto" or "" reports ok=false (size to content); a decimal string
// resolves to its value. Non-numeric values report ok=false.
func ParseDimension(v string, fillWidth int) (int, bool) {
	switch v {
	case "", "auto":
		return 0, false
	case "fill":
		if fillWidth <= 0 {
			return 0, false
		}
		return fillWidth, true
	}
	if n, ok := ParseDecimal(v); ok {
		return n, true
	}
	return 0, false
}

// ResolveConstraint resolves an area sizing constraint ("N" absolute or "N%"
// percent of total) to a concrete number of cells/lines. It reports ok=false
// for an empty or unparseable value, meaning unconstrained.
func ResolveConstraint(v string, total int) (int, bool) {
	if v == "" {
		return 0, false
	}
	if strings.HasSuffix(v, "%") {
		n, ok := ParseDecimal(strings.TrimSuffix(v, "%"))
		if !ok || n < 0 {
			return 0, false
		}
		return total * n / 100, true
	}
	n, ok := ParseDecimal(v)
	if !ok {
		return 0, false
	}
	return n, true
}

// ParseDecimal parses a non-negative decimal integer string. It reports
// ok=false for empty strings, negatives, non-digit characters, or a value too
// large to represent in an int (rather than silently wrapping to a negative).
func ParseDecimal(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	const maxInt = int(^uint(0) >> 1)
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		d := int(ch - '0')
		if n > (maxInt-d)/10 {
			return 0, false
		}
		n = n*10 + d
	}
	return n, true
}

// ExpandSideCSS renders a cell-count side list as a CSS padding/margin
// shorthand value in the given unit ("ch" for widths, "lh" for heights), or ""
// when unset. It exists so the HTML renderer does not re-derive the 1/2/4
// expansion that PropSides already owns.
func ExpandSideCSS(v []int, unit string) string {
	top, right, bottom, left, ok := PropSides(v)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d%s %d%s %d%s %d%s", top, unit, right, unit, bottom, unit, left, unit)
}
