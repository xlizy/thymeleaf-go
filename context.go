package thymeleaf

import "os"

// Context is the data container used while rendering a template.
//
// The shape intentionally mirrors the parts of Thymeleaf that are useful for
// mail templates:
//   - Variables are read by ${...}.
//   - Selection is read by *{...}; th:object can override it for a subtree.
//   - Messages are read by #{...}.
//   - BaseURL is used by @{...} when the template produces absolute links.
//   - Fragments are read by ~{...}.
type Context struct {
	Variables map[string]any
	Messages  map[string]string
	Fragments map[string]string
	BaseURL   string
	Root      any
	Selection any
}

// NewContext creates a render context from a variable map.
func NewContext(variables map[string]any) Context {
	return Context{
		Variables: variables,
		Root:      variables,
		Selection: variables,
	}
}

// Engine renders Thymeleaf-like HTML templates.
//
// The current implementation deliberately targets a practical subset of
// org.thymeleaf for mail templates. It does not try to be a complete OGNL or
// SpringEL implementation.
type Engine struct{}

// New creates a template engine instance.
func New() *Engine {
	return &Engine{}
}

// ProcessString renders a template from either a Context, a map, or a struct.
//
// When data is a Context, every field is used as-is. When it is a map, keys are
// exposed to ${...}. When it is any other value, it is exposed both as Root and
// as the initial Selection, so expressions like ${name} and *{name} can resolve
// against the value's fields.
func (e *Engine) ProcessString(template string, data any) (string, error) {
	switch v := data.(type) {
	case Context:
		return e.ProcessStringContext(template, v)
	case map[string]any:
		return e.ProcessStringContext(template, NewContext(v))
	default:
		return e.ProcessStringContext(template, Context{
			Variables: map[string]any{"root": data},
			Root:      data,
			Selection: data,
		})
	}
}

// ProcessFile reads a template file and renders it with ProcessString.
func (e *Engine) ProcessFile(filename string, data any) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return e.ProcessString(string(content), data)
}

// ProcessFileContext reads a template file and renders it with ProcessStringContext.
func (e *Engine) ProcessFileContext(filename string, ctx Context) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return e.ProcessStringContext(string(content), ctx)
}

// ProcessStringContext renders a template using an explicit Context.
func (e *Engine) ProcessStringContext(template string, ctx Context) (string, error) {
	if ctx.Variables == nil {
		ctx.Variables = map[string]any{}
	}
	if ctx.Root == nil {
		ctx.Root = ctx.Variables
	}
	if ctx.Selection == nil {
		ctx.Selection = ctx.Root
	}

	doc, err := parseHTML(template)
	if err != nil {
		return "", err
	}
	return e.renderChildren(doc.Children, newScope(ctx))
}
