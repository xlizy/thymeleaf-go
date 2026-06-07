package thymeleaf

import (
	"html"
	"strings"
	"unicode"
)

type nodeType int

const (
	documentNode nodeType = iota
	elementNode
	textNode
	rawNode
)

type attr struct {
	Name     string
	Value    string
	HasValue bool
}

type node struct {
	Type        nodeType
	Data        string
	Attr        []attr
	Children    []*node
	SelfClosing bool
}

var htmlVoidTags = map[string]bool{
	"area":   true,
	"base":   true,
	"br":     true,
	"col":    true,
	"embed":  true,
	"hr":     true,
	"img":    true,
	"input":  true,
	"link":   true,
	"meta":   true,
	"param":  true,
	"source": true,
	"track":  true,
	"wbr":    true,
}

// parseHTML builds a small DOM tree that is good enough for ordinary HTML mail
// templates. It accepts common HTML syntax, including unquoted attributes and
// void tags, because mail templates often need to remain directly previewable in
// a browser.
func parseHTML(input string) (*node, error) {
	root := &node{Type: documentNode}
	stack := []*node{root}

	for i := 0; i < len(input); {
		if input[i] != '<' {
			next := strings.IndexByte(input[i:], '<')
			if next < 0 {
				appendChild(stack[len(stack)-1], &node{Type: textNode, Data: input[i:]})
				break
			}
			appendChild(stack[len(stack)-1], &node{Type: textNode, Data: input[i : i+next]})
			i += next
			continue
		}

		if strings.HasPrefix(input[i:], "<!--") {
			end := strings.Index(input[i+4:], "-->")
			if end < 0 {
				appendChild(stack[len(stack)-1], &node{Type: rawNode, Data: input[i:]})
				break
			}
			endIndex := i + 4 + end + len("-->")
			appendChild(stack[len(stack)-1], &node{Type: rawNode, Data: input[i:endIndex]})
			i = endIndex
			continue
		}

		end := findTagEnd(input, i)
		if end < 0 {
			appendChild(stack[len(stack)-1], &node{Type: textNode, Data: input[i:]})
			break
		}

		tagBody := strings.TrimSpace(input[i+1 : end])
		if tagBody == "" {
			appendChild(stack[len(stack)-1], &node{Type: textNode, Data: input[i : end+1]})
			i = end + 1
			continue
		}

		if strings.HasPrefix(tagBody, "!") || strings.HasPrefix(tagBody, "?") {
			appendChild(stack[len(stack)-1], &node{Type: rawNode, Data: input[i : end+1]})
			i = end + 1
			continue
		}

		if strings.HasPrefix(tagBody, "/") {
			fields := strings.Fields(strings.TrimSpace(tagBody[1:]))
			if len(fields) == 0 {
				i = end + 1
				continue
			}
			name := strings.ToLower(fields[0])
			for len(stack) > 1 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if strings.EqualFold(top.Data, name) {
					break
				}
			}
			i = end + 1
			continue
		}

		selfClosing := strings.HasSuffix(tagBody, "/")
		if selfClosing {
			tagBody = strings.TrimSpace(strings.TrimSuffix(tagBody, "/"))
		}
		name, rest := readTagName(tagBody)
		if name == "" {
			appendChild(stack[len(stack)-1], &node{Type: textNode, Data: input[i : end+1]})
			i = end + 1
			continue
		}

		el := &node{
			Type:        elementNode,
			Data:        name,
			Attr:        parseAttrs(rest),
			SelfClosing: selfClosing || htmlVoidTags[strings.ToLower(name)],
		}
		appendChild(stack[len(stack)-1], el)
		if !el.SelfClosing {
			stack = append(stack, el)
		}
		i = end + 1
	}

	return root, nil
}

func appendChild(parent *node, child *node) {
	if child.Type == textNode && child.Data == "" {
		return
	}
	parent.Children = append(parent.Children, child)
}

func findTagEnd(input string, start int) int {
	var quote byte
	for i := start + 1; i < len(input); i++ {
		c := input[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '>' {
			return i
		}
	}
	return -1
}

func readTagName(body string) (string, string) {
	for i, r := range body {
		if unicode.IsSpace(r) || r == '/' {
			return body[:i], body[i:]
		}
	}
	return body, ""
}

func parseAttrs(input string) []attr {
	var attrs []attr
	for i := 0; i < len(input); {
		for i < len(input) && unicode.IsSpace(rune(input[i])) {
			i++
		}
		if i >= len(input) {
			break
		}

		nameStart := i
		for i < len(input) && !unicode.IsSpace(rune(input[i])) && input[i] != '=' && input[i] != '/' {
			i++
		}
		name := strings.TrimSpace(input[nameStart:i])
		if name == "" {
			i++
			continue
		}

		for i < len(input) && unicode.IsSpace(rune(input[i])) {
			i++
		}
		if i >= len(input) || input[i] != '=' {
			attrs = append(attrs, attr{Name: name})
			continue
		}

		i++
		for i < len(input) && unicode.IsSpace(rune(input[i])) {
			i++
		}

		var value string
		if i < len(input) && (input[i] == '"' || input[i] == '\'') {
			quote := input[i]
			i++
			valueStart := i
			for i < len(input) && input[i] != quote {
				i++
			}
			value = input[valueStart:i]
			if i < len(input) {
				i++
			}
		} else {
			valueStart := i
			for i < len(input) && !unicode.IsSpace(rune(input[i])) {
				i++
			}
			value = input[valueStart:i]
		}

		attrs = append(attrs, attr{Name: name, Value: html.UnescapeString(value), HasValue: true})
	}
	return attrs
}
