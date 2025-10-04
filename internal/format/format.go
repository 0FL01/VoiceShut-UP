package format

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

func FormatHTML(text string) string {
	reCodeBlock := regexp.MustCompile("(?s)```(\\w+)?\n(.*?)\n```")
	text = reCodeBlock.ReplaceAllStringFunc(text, func(match string) string {
		parts := reCodeBlock.FindStringSubmatch(match)
		lang, code := "", parts[2]
		if len(parts) > 1 {
			lang = parts[1]
		}
		return fmt.Sprintf(`<pre><code class="language-%s">%s</code></pre>`, lang, html.EscapeString(strings.TrimSpace(code)))
	})
	reBold := regexp.MustCompile(`\*\*(.*?)\*\*`)
	text = reBold.ReplaceAllString(text, `<b>$1</b>`)
	reItalic := regexp.MustCompile(`\*(.*?)\*`)
	text = reItalic.ReplaceAllString(text, `<i>$1</i>`)
	reCode := regexp.MustCompile("`([^`]+)`")
	text = reCode.ReplaceAllString(text, `<code>$1</code>`)
	reListItem := regexp.MustCompile(`(?m)^\* `)
	text = reListItem.ReplaceAllString(text, "• ")
	return text
}

func SplitMessage(message string, maxLen int) []string {
	if len(message) <= maxLen {
		return []string{message}
	}
	var parts []string
	for len(message) > 0 {
		if len(message) <= maxLen {
			parts = append(parts, message)
			break
		}
		splitPos := strings.LastIndex(message[:maxLen], "\n")
		if splitPos == -1 {
			splitPos = strings.LastIndex(message[:maxLen], " ")
		}
		if splitPos == -1 {
			splitPos = maxLen
		}
		parts = append(parts, message[:splitPos])
		message = strings.TrimSpace(message[splitPos:])
	}
	return parts
}


