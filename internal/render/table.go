package render

import (
	"strings"
	"unicode/utf8"
)

// Box-drawing characters for table rendering.
const (
	boxTopLeft     = "┌"
	boxTopRight    = "┐"
	boxBottomLeft  = "└"
	boxBottomRight = "┘"
	boxHorizontal  = "─"
	boxVertical    = "│"
	boxTopTee      = "┬"
	boxBottomTee   = "┴"
	boxLeftTee     = "├"
	boxRightTee    = "┤"
	boxCross       = "┼"
)

// isTableRow checks if a line looks like a markdown table row: | col | col |
func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) > 1 && trimmed[0] == '|' && trimmed[len(trimmed)-1] == '|'
}

// isSeparatorRow checks if a line is a markdown table separator: |---|---|
func isSeparatorRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !isTableRow(trimmed) {
		return false
	}
	// Remove pipes and check if only dashes, colons, spaces remain.
	inner := trimmed[1 : len(trimmed)-1]
	for _, r := range inner {
		switch r {
		case '-', ':', '|', ' ':
			continue
		default:
			return false
		}
	}
	return true
}

// parseCells splits a table row into trimmed cell values.
func parseCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	// Remove leading and trailing pipes.
	if len(trimmed) > 1 && trimmed[0] == '|' {
		trimmed = trimmed[1:]
	}
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '|' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	parts := strings.Split(trimmed, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// renderBoxTable renders a set of markdown table lines (header, separator, data rows)
// as a box-drawing table. Returns the rendered string.
func renderBoxTable(lines []string) string {
	if len(lines) < 2 {
		// Not enough for a real table — return as-is.
		return strings.Join(lines, "\n") + "\n"
	}

	// Parse all rows (skip separator rows).
	var rows [][]string
	for _, line := range lines {
		if isSeparatorRow(line) {
			continue
		}
		rows = append(rows, parseCells(line))
	}
	if len(rows) == 0 {
		return strings.Join(lines, "\n") + "\n"
	}

	// Determine column count and widths.
	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	if numCols == 0 {
		return strings.Join(lines, "\n") + "\n"
	}

	// Normalize rows to have the same number of columns.
	for i := range rows {
		for len(rows[i]) < numCols {
			rows[i] = append(rows[i], "")
		}
	}

	// Calculate max width for each column (using display width).
	colWidths := make([]int, numCols)
	for _, row := range rows {
		for c, cell := range row {
			w := displayWidth(cell)
			if w > colWidths[c] {
				colWidths[c] = w
			}
		}
	}

	// Build the table.
	var b strings.Builder

	// Top border: ┌───┬───┐
	b.WriteString("  " + boxTopLeft)
	for c, w := range colWidths {
		b.WriteString(strings.Repeat(boxHorizontal, w+2))
		if c < numCols-1 {
			b.WriteString(boxTopTee)
		}
	}
	b.WriteString(boxTopRight + "\n")

	for i, row := range rows {
		// Data row: │ val │ val │
		b.WriteString("  " + boxVertical)
		for c, cell := range row {
			padding := colWidths[c] - displayWidth(cell)
			b.WriteString(" " + cell + strings.Repeat(" ", padding) + " " + boxVertical)
		}
		b.WriteString("\n")

		// Separator after header (first row): ├───┼───┤
		// Also between every row for consistency with Pi TUI.
		if i < len(rows)-1 {
			b.WriteString("  " + boxLeftTee)
			for c, w := range colWidths {
				b.WriteString(strings.Repeat(boxHorizontal, w+2))
				if c < numCols-1 {
					b.WriteString(boxCross)
				}
			}
			b.WriteString(boxRightTee + "\n")
		}
	}

	// Bottom border: └───┴───┘
	b.WriteString("  " + boxBottomLeft)
	for c, w := range colWidths {
		b.WriteString(strings.Repeat(boxHorizontal, w+2))
		if c < numCols-1 {
			b.WriteString(boxBottomTee)
		}
	}
	b.WriteString(boxBottomRight + "\n")

	return b.String()
}

// displayWidth returns the visible width of a string, accounting for
// multi-byte UTF-8 characters. This is a simplification that counts
// runes — it doesn't handle East Asian wide characters or zero-width
// joiners, but is sufficient for typical table content.
func displayWidth(s string) int {
	return utf8.RuneCountInString(s)
}
