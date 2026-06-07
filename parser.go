package thymeleaf

import (
	"html"
	"strings"
	"unicode"
)

// nodeType 标识解析后节点的类型。
//
// 解析器只区分文档、元素、文本和原始节点，足以支撑当前 Thymeleaf 子集的渲染。
type nodeType int

const (
	documentNode nodeType = iota
	elementNode
	textNode
	rawNode
)

// attr 表示一个 HTML 属性。
//
// HasValue 用于区分 `disabled` 这类布尔属性和 `disabled=""` 这类显式空值属性。
type attr struct {
	Name     string
	Value    string
	HasValue bool
}

// node 是模板解析后的轻量节点结构。
//
// Data 在元素节点里表示标签名，在文本或原始节点里表示文本内容。
type node struct {
	Type        nodeType
	Data        string
	Attr        []attr
	Children    []*node
	SelfClosing bool
}

// htmlVoidTags 记录 HTML 中不需要结束标签的 void 元素。
//
// 渲染时这些标签会输出为 `<img>`、`<br>` 这类形式，而不是 `<img></img>`。
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

// parseHTML 把模板字符串解析为轻量 DOM 树。
//
// 这个解析器面向普通 HTML 邮件模板，支持常见标签、注释、doctype、未加引号属性和 void
// 标签。它的目标不是完整实现 HTML5 解析算法，而是在不引入第三方依赖的前提下，让可浏览器
// 预览的 Thymeleaf 风格模板可以被服务端渲染。
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

// appendChild 向父节点追加子节点。
//
// 空文本节点没有渲染价值，直接忽略；其它节点按原始顺序保留。
func appendChild(parent *node, child *node) {
	if child.Type == textNode && child.Data == "" {
		return
	}
	parent.Children = append(parent.Children, child)
}

// findTagEnd 查找从 start 位置开始的标签结束符 `>`。
//
// 查找过程中会跳过属性引号里的 `>`，避免 `<a title="1 > 0">` 这种内容被错误截断。
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

// readTagName 从标签体里读取标签名和剩余属性字符串。
//
// 例如 `div class="x"` 会返回 `div` 与 ` class="x"`。
func readTagName(body string) (string, string) {
	for i, r := range body {
		if unicode.IsSpace(r) || r == '/' {
			return body[:i], body[i:]
		}
	}
	return body, ""
}

// parseAttrs 解析标签属性。
//
// 支持双引号、单引号、未加引号和布尔属性；属性值会先做 html.UnescapeString，
// 这样模板里的 `&amp;` 等实体在表达式处理前能还原为普通字符。
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
