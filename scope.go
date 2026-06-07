package thymeleaf

type scope struct {
	ctx       Context
	vars      map[string]any
	selection any
}

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

func (s *scope) withVar(name string, value any) *scope {
	next := s.clone()
	next.vars[name] = value
	next.vars["this"] = value
	return next
}

func (s *scope) withSelection(selection any) *scope {
	next := s.clone()
	next.selection = selection
	next.vars["this"] = selection
	return next
}

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

func (s *scope) lookup(name string) (any, bool) {
	v, ok := s.vars[name]
	return v, ok
}
