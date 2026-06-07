package thymeleaf

// scope 表示一次渲染过程中的变量作用域。
//
// 它在 Context 基础上维护局部变量和当前选择对象，供 th:each、th:object 以及表达式解析使用。
type scope struct {
	ctx       Context
	vars      map[string]any
	selection any
}

// newScope 根据 Context 创建根作用域。
//
// 根作用域会拷贝 Variables，并补充 root 与 this 两个便捷变量：
//   - root 指向 Context.Root。
//   - this 指向当前 Selection，方便表达式直接读取当前对象。
func newScope(ctx Context) *scope {
	vars := make(map[string]any, len(ctx.Variables)+2)
	for k, v := range ctx.Variables {
		vars[k] = v
	}
	vars["root"] = ctx.Root
	vars["this"] = ctx.Selection
	return &scope{
		ctx:       ctx,
		vars:      vars,
		selection: ctx.Selection,
	}
}

// withVar 创建包含额外局部变量的新作用域。
//
// th:each 会使用这个方法注入当前迭代项和状态变量。这里不修改原 scope，
// 避免兄弟节点之间的局部变量互相污染。
func (s *scope) withVar(name string, value any) *scope {
	next := s.clone()
	next.vars[name] = value
	next.vars["this"] = value
	return next
}

// withSelection 创建使用新选择对象的作用域。
//
// th:object 会使用这个方法，让子树中的 *{...} 表达式改为基于指定对象取值。
func (s *scope) withSelection(selection any) *scope {
	next := s.clone()
	next.selection = selection
	next.vars["this"] = selection
	return next
}

// clone 深拷贝作用域中的变量 map。
//
// Context 本身仍然共享，因为 Messages、Fragments、BaseURL 这类配置在一次渲染中应该保持一致。
func (s *scope) clone() *scope {
	vars := make(map[string]any, len(s.vars)+1)
	for k, v := range s.vars {
		vars[k] = v
	}
	return &scope{
		ctx:       s.ctx,
		vars:      vars,
		selection: s.selection,
	}
}

// lookup 从当前作用域读取变量。
//
// 这个方法只查局部变量表，不做结构体字段或 map 属性解析；属性解析由 evalPath 负责。
func (s *scope) lookup(name string) (any, bool) {
	v, ok := s.vars[name]
	return v, ok
}
