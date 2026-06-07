package thymeleaf

import (
	"fmt"
	"html"
	"strings"
)

var standardAttributeProcessors = []struct {
	ThName   string
	HTMLName string
}{
	{ThName: "th:id", HTMLName: "id"},
	{ThName: "th:value", HTMLName: "value"},
	{ThName: "th:href", HTMLName: "href"},
	{ThName: "th:src", HTMLName: "src"},
}

// renderChildren 顺序渲染一组子节点。
//
// HTML 模板里大多数内容都是兄弟节点串联输出，因此这里负责维护输出顺序，并把每个节点的
// 错误原样向上传递，方便上层定位具体模板问题。
func (e *Engine) renderChildren(children []*node, s *scope) (string, error) {
	var b strings.Builder
	for _, child := range children {
		rendered, err := e.renderNode(child, s)
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
	}
	return b.String(), nil
}

// renderNode 根据节点类型选择渲染策略。
//
// th:each 需要重复“当前元素本身”，所以必须在普通元素渲染之前处理；其它元素再进入
// renderElement 统一处理条件、属性和内容。
func (e *Engine) renderNode(n *node, s *scope) (string, error) {
	switch n.Type {
	case documentNode:
		return e.renderChildren(n.Children, s)
	case textNode, rawNode:
		return n.Data, nil
	case elementNode:
		if eachSpec, ok := attrValue(n.Attr, "th:each"); ok {
			return e.renderEach(n, s, eachSpec)
		}
		return e.renderElement(n, s)
	default:
		return "", nil
	}
}

// renderElement 渲染单个 HTML 元素节点。
//
// 处理顺序刻意贴近 Thymeleaf 的使用习惯：
//  1. 先处理 th:object，确定当前子树的选择对象。
//  2. 再处理 th:if/th:unless，条件不满足时直接跳过整个元素。
//  3. 渲染普通属性与 th:* 属性转换结果。
//  4. 渲染 th:text/th:utext 或递归渲染子节点。
func (e *Engine) renderElement(n *node, s *scope) (string, error) {
	activeScope := s
	if objectExpr, ok := attrValue(n.Attr, "th:object"); ok {
		objectValue, err := e.evalAny(objectExpr, s)
		if err != nil {
			return "", fmt.Errorf("render th:object on <%s>: %w", n.Data, err)
		}
		activeScope = s.withSelection(objectValue)
	}

	visible, err := e.shouldRender(n.Attr, activeScope)
	if err != nil {
		return "", fmt.Errorf("render conditions on <%s>: %w", n.Data, err)
	}
	if !visible {
		return "", nil
	}

	attrs, err := e.renderAttrs(n.Attr, activeScope)
	if err != nil {
		return "", fmt.Errorf("render attributes on <%s>: %w", n.Data, err)
	}

	body, replaceBody, err := e.renderBody(n, activeScope)
	if err != nil {
		return "", fmt.Errorf("render body on <%s>: %w", n.Data, err)
	}
	if !replaceBody {
		body, err = e.renderChildren(n.Children, activeScope)
		if err != nil {
			return "", err
		}
	}

	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(n.Data)
	b.WriteString(renderAttrString(attrs))

	isVoid := htmlVoidTags[strings.ToLower(n.Data)]
	if n.SelfClosing && body == "" {
		if isVoid {
			b.WriteByte('>')
		} else {
			b.WriteString(" />")
		}
		return b.String(), nil
	}

	b.WriteByte('>')
	b.WriteString(body)
	b.WriteString("</")
	b.WriteString(n.Data)
	b.WriteByte('>')
	return b.String(), nil
}

// shouldRender 根据 th:if 与 th:unless 判断当前元素是否应该输出。
//
// 两个属性同时存在时会同时生效：th:if 必须为真，th:unless 必须为假。
// 真假判断统一交给 isTruthy，以便字符串、数字、集合等类型有一致语义。
func (e *Engine) shouldRender(attrs []attr, s *scope) (bool, error) {
	if expr, ok := attrValue(attrs, "th:if"); ok {
		value, err := e.evalAny(expr, s)
		if err != nil {
			return false, fmt.Errorf("render th:if: %w", err)
		}
		if !isTruthy(value) {
			return false, nil
		}
	}

	if expr, ok := attrValue(attrs, "th:unless"); ok {
		value, err := e.evalAny(expr, s)
		if err != nil {
			return false, fmt.Errorf("render th:unless: %w", err)
		}
		if isTruthy(value) {
			return false, nil
		}
	}

	return true, nil
}

// renderAttrs 渲染元素属性。
//
// 普通 HTML 属性会保留，所有 th:* 属性不会直接输出；受支持的 th:* 属性会转换成对应
// HTML 属性，例如 th:href -> href，th:classappend -> class 追加。
func (e *Engine) renderAttrs(attrs []attr, s *scope) ([]attr, error) {
	out := make([]attr, 0, len(attrs))
	for _, a := range attrs {
		if isThymeleafAttr(a.Name) || strings.EqualFold(a.Name, "xmlns:th") {
			continue
		}
		out = append(out, a)
	}

	for _, processor := range standardAttributeProcessors {
		expr, ok := attrValue(attrs, processor.ThName)
		if !ok {
			continue
		}
		value, err := e.evalString(expr, s)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", processor.ThName, err)
		}
		out = setAttr(out, processor.HTMLName, value)
	}

	if expr, ok := attrValue(attrs, "th:classappend"); ok {
		value, err := e.evalString(expr, s)
		if err != nil {
			return nil, fmt.Errorf("render th:classappend: %w", err)
		}
		out = appendClass(out, value)
	}
	return out, nil
}

// renderBody 渲染元素内容。
//
// th:text 会 HTML 转义，适合普通文本；th:utext 不转义，适合可信 HTML 片段。
// 返回值 replaced 表示当前元素内容是否已被 th:text/th:utext 接管。
func (e *Engine) renderBody(n *node, s *scope) (body string, replaced bool, err error) {
	if expr, ok := attrValue(n.Attr, "th:text"); ok {
		value, err := e.evalAny(expr, s)
		if err != nil {
			return "", true, fmt.Errorf("render th:text: %w", err)
		}
		return html.EscapeString(valueToString(value)), true, nil
	}

	if expr, ok := attrValue(n.Attr, "th:utext"); ok {
		value, err := e.evalAny(expr, s)
		if err != nil {
			return "", true, fmt.Errorf("render th:utext: %w", err)
		}
		return valueToString(value), true, nil
	}

	return "", false, nil
}

// attrValue 从属性列表中按名称读取属性值。
//
// HTML 属性名大小写不敏感，因此这里使用 EqualFold 匹配，兼容 th:Text 这类不规范输入。
func attrValue(attrs []attr, name string) (string, bool) {
	for _, a := range attrs {
		if strings.EqualFold(a.Name, name) {
			return a.Value, true
		}
	}
	return "", false
}

// isThymeleafAttr 判断属性是否为 th:* 模板属性。
//
// 模板属性只参与服务端渲染，不应该直接出现在最终 HTML 邮件内容里。
func isThymeleafAttr(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "th:")
}

// setAttr 设置或新增一个 HTML 属性。
//
// 如果元素原本已经有同名属性，表达式渲染结果会覆盖原值；这符合 Thymeleaf 的常见行为，
// 例如 href 与 th:href 同时存在时，th:href 是运行时真实值。
func setAttr(attrs []attr, name string, value string) []attr {
	for i := range attrs {
		if strings.EqualFold(attrs[i].Name, name) {
			attrs[i].Value = value
			attrs[i].HasValue = true
			return attrs
		}
	}
	return append(attrs, attr{Name: name, Value: value, HasValue: true})
}

// appendClass 追加 class 属性值。
//
// th:classappend 的表达式结果为空时不做任何修改；原元素没有 class 时会创建 class 属性，
// 已有 class 时用一个空格拼接，避免输出多余空白。
func appendClass(attrs []attr, value string) []attr {
	value = strings.TrimSpace(value)
	if value == "" {
		return attrs
	}

	for i := range attrs {
		if strings.EqualFold(attrs[i].Name, "class") {
			current := strings.TrimSpace(attrs[i].Value)
			if current == "" {
				attrs[i].Value = value
			} else {
				attrs[i].Value = current + " " + value
			}
			attrs[i].HasValue = true
			return attrs
		}
	}
	return append(attrs, attr{Name: "class", Value: value, HasValue: true})
}

// renderAttrString 把属性列表序列化为 HTML 属性字符串。
//
// 属性值会进行 HTML 转义，避免用户数据进入属性后破坏 HTML 结构。
func renderAttrString(attrs []attr) string {
	var b strings.Builder
	for _, a := range attrs {
		b.WriteByte(' ')
		b.WriteString(a.Name)
		if !a.HasValue {
			continue
		}
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(a.Value))
		b.WriteByte('"')
	}
	return b.String()
}
