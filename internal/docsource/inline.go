package docsource

// MaskInlineCode hides matched code spans, including multiple-backtick delimiters,
// without hiding real links elsewhere on the same line. Unmatched delimiters
// remain ordinary text. This checker is deliberately not a full Markdown parser.
func MaskInlineCode(line string) string {
	masked := []byte(line)
	for start := 0; start < len(line); {
		if line[start] != '`' {
			start++
			continue
		}
		end := start
		for end < len(line) && line[end] == '`' {
			end++
		}
		width := end - start
		closed := -1
		for cursor := end; cursor < len(line); {
			if line[cursor] != '`' {
				cursor++
				continue
			}
			next := cursor
			for next < len(line) && line[next] == '`' {
				next++
			}
			if next-cursor == width {
				closed = next
				break
			}
			cursor = next
		}
		if closed < 0 {
			start = end
			continue
		}
		for i := start; i < closed; i++ {
			masked[i] = ' '
		}
		start = closed
	}
	return string(masked)
}
