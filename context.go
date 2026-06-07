package thymeleaf

import "os"

// Context 是模板渲染时使用的数据上下文。
//
// 这个结构只抽象邮件模板常用的 Thymeleaf 能力：
//   - Variables：供 ${...} 变量表达式读取。
//   - Selection：供 *{...} 选择对象表达式读取，子树里的 th:object 会临时覆盖它。
//   - Messages：供 #{...} 消息表达式读取，用于国际化文案或统一文案模板。
//   - BaseURL：供 @{...} 链接表达式拼接绝对地址，常用于邮件里的按钮链接。
//   - Fragments：供 ~{...} 片段表达式读取，用于页脚、签名等可复用 HTML 片段。
type Context struct {
	Variables map[string]any
	Messages  map[string]string
	Fragments map[string]string
	BaseURL   string
	Root      any
	Selection any
}

// NewContext 根据变量 map 创建默认渲染上下文。
//
// 默认情况下 Variables、Root、Selection 都指向传入 map，因此 `${name}` 与 `*{name}`
// 在没有显式 th:object 的情况下都可以读取同一份数据。
func NewContext(variables map[string]any) Context {
	return Context{
		Variables: variables,
		Root:      variables,
		Selection: variables,
	}
}

// Engine 是 Thymeleaf 风格 HTML 模板的渲染引擎。
//
// 当前实现目标是邮件模板常用的 Thymeleaf 子集，而不是完整移植 Java 版
// org.thymeleaf，也不会实现完整的 OGNL 或 SpringEL。
type Engine struct{}

// New 创建一个模板引擎实例。
//
// Engine 当前不保存可变状态，因此实例可以复用；后续如果增加模板缓存，也会尽量保持
// ProcessString/ProcessFile 的调用方式稳定。
func New() *Engine {
	return &Engine{}
}

// ProcessString 使用任意数据渲染模板字符串。
//
// data 的处理规则如下：
//   - Context：直接使用调用方提供的完整上下文。
//   - map[string]any：作为 Variables，并同时作为 Root/Selection。
//   - 其它类型：作为 Root 和初始 Selection，同时以 root 变量暴露。
//
// 这样既能满足 `${user.name}` 这类 map 数据，也能支持直接传入结构体后使用 `*{name}`。
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

// ProcessFile 读取模板文件并使用 ProcessString 渲染。
//
// 这个入口适合邮件模板落在磁盘目录里的场景，调用方只需要传入模板文件路径和业务数据。
func (e *Engine) ProcessFile(filename string, data any) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return e.ProcessString(string(content), data)
}

// ProcessFileContext 读取模板文件并使用显式 Context 渲染。
//
// 当需要同时提供 Variables、Messages、Fragments、BaseURL 等信息时，优先使用这个方法。
func (e *Engine) ProcessFileContext(filename string, ctx Context) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return e.ProcessStringContext(string(content), ctx)
}

// ProcessStringContext 使用显式 Context 渲染模板字符串。
//
// 这里会补齐 Context 的默认值，随后把 HTML 解析成轻量节点树，再进入渲染流程。
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
