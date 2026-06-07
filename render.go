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

func attrValue(attrs []attr, name string) (string, bool) {
	for _, a := range attrs {
		if strings.EqualFold(a.Name, name) {
			return a.Value, true
		}
	}
	return "", false
}

func isThymeleafAttr(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "th:")
}

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
