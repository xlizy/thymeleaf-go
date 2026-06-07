package thymeleaf

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// exprToken 表示从模板文本中读取到的一个表达式 token。
//
// Kind 是表达式类型前缀，例如 `$`、`*`、`#`、`@`、`~`；Body 是花括号内部内容；
// End 是表达式在原始字符串中的结束位置，便于继续扫描后续文本。
type exprToken struct {
	Kind byte
	Body string
	End  int
}

// pathSegment 表示属性路径中的一段。
//
// 例如 `user.addresses[0].city` 会被拆成 user、addresses[0]、city 三段。
type pathSegment struct {
	Name    string
	Indexes []string
}

// evalAny 计算任意模板表达式。
//
// 它既支持完整表达式 `${...}`、`@{...}`，也支持混合写法，例如
// `${active} ? 'on' : ”` 或普通字符串里内嵌多个表达式。
func (e *Engine) evalAny(raw string, s *scope) (any, error) {
	expr := strings.TrimSpace(raw)
	if expr == "" {
		return "", nil
	}

	if token, ok := readExpressionAt(expr, 0); ok && token.End == len(expr) {
		return e.evalExpression(token.Kind, token.Body, s)
	}

	if value, ok, err := e.evalMixedStandardExpression(expr, s); ok || err != nil {
		return value, err
	}

	if unquoted, ok := unquoteLiteral(expr); ok {
		return unquoted, nil
	}
	if expr == "true" {
		return true, nil
	}
	if expr == "false" {
		return false, nil
	}
	if expr == "nil" || expr == "null" {
		return nil, nil
	}
	if i, err := strconv.ParseInt(expr, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(expr, 64); err == nil {
		return f, nil
	}

	return e.evalInlineString(raw, s)
}

// evalString 计算表达式并把结果转换成字符串。
//
// HTML 属性值、th:classappend 等最终需要字符串，因此统一走这个方法做转换。
func (e *Engine) evalString(raw string, s *scope) (string, error) {
	value, err := e.evalAny(raw, s)
	if err != nil {
		return "", err
	}
	return valueToString(value), nil
}

// evalInlineString 渲染包含内联表达式的普通字符串。
//
// 例如 `/orders/${order.id}` 会把 `${order.id}` 替换为变量值，非表达式内容保持原样。
func (e *Engine) evalInlineString(raw string, s *scope) (string, error) {
	var b strings.Builder
	for i := 0; i < len(raw); {
		token, ok := readExpressionAt(raw, i)
		if !ok {
			b.WriteByte(raw[i])
			i++
			continue
		}

		value, err := e.evalExpression(token.Kind, token.Body, s)
		if err != nil {
			return "", err
		}
		b.WriteString(valueToString(value))
		i = token.End
	}
	return b.String(), nil
}

// readExpressionAt 尝试从指定位置读取一个完整表达式。
//
// 支持 `${...}`、`*{...}`、`#{...}`、`@{...}`、`~{...}`。读取时会处理嵌套花括号和
// 引号，避免 `${'a}b'}` 这类内容被提前截断。
func readExpressionAt(input string, start int) (exprToken, bool) {
	if start+2 > len(input) {
		return exprToken{}, false
	}
	kind := input[start]
	if kind != '$' && kind != '*' && kind != '#' && kind != '@' && kind != '~' {
		return exprToken{}, false
	}
	if input[start+1] != '{' {
		return exprToken{}, false
	}

	depth := 1
	var quote byte
	for i := start + 2; i < len(input); i++ {
		c := input[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(input) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return exprToken{Kind: kind, Body: input[start+2 : i], End: i + 1}, true
			}
		}
	}
	return exprToken{}, false
}

// evalExpression 根据表达式前缀分发到具体求值器。
//
// Thymeleaf 的不同表达式语义不同：变量、选择对象、消息、链接和片段都需要不同的数据源。
func (e *Engine) evalExpression(kind byte, body string, s *scope) (any, error) {
	switch kind {
	case '$':
		return e.evalStandardExpression(body, s, s.ctx.Root)
	case '*':
		return e.evalStandardExpression(body, s, s.selection)
	case '#':
		return e.evalMessageExpression(body, s)
	case '@':
		return e.evalURLExpression(body, s)
	case '~':
		return e.evalFragmentExpression(body, s)
	default:
		return "", fmt.Errorf("unsupported expression %c{%s}", kind, body)
	}
}

// evalStandardExpression 计算 ${...} 或 *{...} 内部的标准表达式。
//
// 支持字符串/数字/布尔字面量、属性路径、加号拼接、三元表达式、逻辑表达式和比较表达式。
// root 参数决定属性路径的起点：${...} 使用 Context.Root，*{...} 使用当前 Selection。
func (e *Engine) evalStandardExpression(raw string, s *scope, root any) (any, error) {
	expr := strings.TrimSpace(raw)
	if expr == "" {
		return "", nil
	}

	if conditionExpr, yesExpr, noExpr, ok := splitTernary(expr); ok {
		condition, err := e.evalStandardExpression(conditionExpr, s, root)
		if err != nil {
			return nil, err
		}
		if isTruthy(condition) {
			return e.evalStandardExpression(yesExpr, s, root)
		}
		return e.evalStandardExpression(noExpr, s, root)
	}

	if parts := splitTopLevelWord(expr, "or"); len(parts) > 1 {
		for _, part := range parts {
			value, err := e.evalStandardExpression(part, s, root)
			if err != nil {
				return nil, err
			}
			if isTruthy(value) {
				return true, nil
			}
		}
		return false, nil
	}
	if parts := splitTopLevelOperator(expr, "||"); len(parts) > 1 {
		for _, part := range parts {
			value, err := e.evalStandardExpression(part, s, root)
			if err != nil {
				return nil, err
			}
			if isTruthy(value) {
				return true, nil
			}
		}
		return false, nil
	}

	if parts := splitTopLevelWord(expr, "and"); len(parts) > 1 {
		for _, part := range parts {
			value, err := e.evalStandardExpression(part, s, root)
			if err != nil {
				return nil, err
			}
			if !isTruthy(value) {
				return false, nil
			}
		}
		return true, nil
	}
	if parts := splitTopLevelOperator(expr, "&&"); len(parts) > 1 {
		for _, part := range parts {
			value, err := e.evalStandardExpression(part, s, root)
			if err != nil {
				return nil, err
			}
			if !isTruthy(value) {
				return false, nil
			}
		}
		return true, nil
	}

	if strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!=") {
		value, err := e.evalStandardExpression(strings.TrimSpace(expr[1:]), s, root)
		if err != nil {
			return nil, err
		}
		return !isTruthy(value), nil
	}
	if strings.HasPrefix(expr, "not ") {
		value, err := e.evalStandardExpression(strings.TrimSpace(expr[len("not "):]), s, root)
		if err != nil {
			return nil, err
		}
		return !isTruthy(value), nil
	}

	for _, operator := range []string{"==", "!=", ">=", "<=", ">", "<"} {
		if left, right, ok := splitComparison(expr, operator); ok {
			leftValue, err := e.evalStandardExpression(left, s, root)
			if err != nil {
				return nil, err
			}
			rightValue, err := e.evalStandardExpression(right, s, root)
			if err != nil {
				return nil, err
			}
			return compareValues(leftValue, rightValue, operator), nil
		}
	}

	parts := splitTopLevel(expr, '+')
	if len(parts) > 1 {
		var b strings.Builder
		for _, part := range parts {
			value, err := e.evalStandardExpression(part, s, root)
			if err != nil {
				return nil, err
			}
			b.WriteString(valueToString(value))
		}
		return b.String(), nil
	}

	if unquoted, ok := unquoteLiteral(expr); ok {
		return unquoted, nil
	}
	if expr == "true" {
		return true, nil
	}
	if expr == "false" {
		return false, nil
	}
	if expr == "nil" || expr == "null" {
		return nil, nil
	}
	if i, err := strconv.ParseInt(expr, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(expr, 64); err == nil {
		return f, nil
	}

	return e.evalPath(expr, s, root)
}

// evalMixedStandardExpression 计算表达式外层混合了操作符的写法。
//
// 模板作者常写 `th:classappend="${active} ? 'on' : ”"` 或
// `th:if="${count} > 1"`。这类写法不是单个 `${...}` 包裹全部内容，所以需要先把
// 两侧表达式分别求值，再执行逻辑、比较或三元运算。
func (e *Engine) evalMixedStandardExpression(raw string, s *scope) (any, bool, error) {
	expr := strings.TrimSpace(raw)
	if expr == "" {
		return "", true, nil
	}

	if conditionExpr, yesExpr, noExpr, ok := splitTernary(expr); ok {
		condition, err := e.evalAny(conditionExpr, s)
		if err != nil {
			return nil, true, err
		}
		if isTruthy(condition) {
			value, err := e.evalAny(yesExpr, s)
			return value, true, err
		}
		value, err := e.evalAny(noExpr, s)
		return value, true, err
	}

	if parts := splitTopLevelWord(expr, "or"); len(parts) > 1 {
		for _, part := range parts {
			value, err := e.evalAny(part, s)
			if err != nil {
				return nil, true, err
			}
			if isTruthy(value) {
				return true, true, nil
			}
		}
		return false, true, nil
	}
	if parts := splitTopLevelOperator(expr, "||"); len(parts) > 1 {
		for _, part := range parts {
			value, err := e.evalAny(part, s)
			if err != nil {
				return nil, true, err
			}
			if isTruthy(value) {
				return true, true, nil
			}
		}
		return false, true, nil
	}

	if parts := splitTopLevelWord(expr, "and"); len(parts) > 1 {
		for _, part := range parts {
			value, err := e.evalAny(part, s)
			if err != nil {
				return nil, true, err
			}
			if !isTruthy(value) {
				return false, true, nil
			}
		}
		return true, true, nil
	}
	if parts := splitTopLevelOperator(expr, "&&"); len(parts) > 1 {
		for _, part := range parts {
			value, err := e.evalAny(part, s)
			if err != nil {
				return nil, true, err
			}
			if !isTruthy(value) {
				return false, true, nil
			}
		}
		return true, true, nil
	}

	if strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!=") {
		value, err := e.evalAny(strings.TrimSpace(expr[1:]), s)
		if err != nil {
			return nil, true, err
		}
		return !isTruthy(value), true, nil
	}
	if strings.HasPrefix(expr, "not ") {
		value, err := e.evalAny(strings.TrimSpace(expr[len("not "):]), s)
		if err != nil {
			return nil, true, err
		}
		return !isTruthy(value), true, nil
	}

	for _, operator := range []string{"==", "!=", ">=", "<=", ">", "<"} {
		if left, right, ok := splitComparison(expr, operator); ok {
			leftValue, err := e.evalAny(left, s)
			if err != nil {
				return nil, true, err
			}
			rightValue, err := e.evalAny(right, s)
			if err != nil {
				return nil, true, err
			}
			return compareValues(leftValue, rightValue, operator), true, nil
		}
	}

	return nil, false, nil
}

// evalPath 按属性路径从变量、map、结构体或当前 root 中读取值。
//
// 第一段路径会优先查当前作用域变量；找不到时再从 root 读取属性。
// 后续路径段都基于上一段结果继续解析。
func (e *Engine) evalPath(raw string, s *scope, root any) (any, error) {
	path := strings.TrimSpace(raw)
	if path == "." || path == "this" {
		return root, nil
	}

	segments, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return root, nil
	}

	var current any
	first := segments[0]
	if value, ok := s.lookup(first.Name); ok {
		current = value
	} else {
		resolved, ok := resolveProperty(root, first.Name)
		if !ok {
			return nil, fmt.Errorf("unknown variable or property %q", first.Name)
		}
		current = resolved
	}

	current, err = applyIndexes(current, first.Indexes, s)
	if err != nil {
		return nil, err
	}

	for _, segment := range segments[1:] {
		next, ok := resolveProperty(current, segment.Name)
		if !ok {
			return nil, fmt.Errorf("unknown property %q on %T", segment.Name, current)
		}
		current, err = applyIndexes(next, segment.Indexes, s)
		if err != nil {
			return nil, err
		}
	}

	return current, nil
}

// parsePath 把点号路径解析为 pathSegment 切片。
//
// 支持 `user.name`、`items[0]`、`map['key']` 这类常见写法，但不实现完整 Java Bean
// 表达式语言。
func parsePath(path string) ([]pathSegment, error) {
	var segments []pathSegment
	for i := 0; i < len(path); {
		if path[i] == '.' {
			i++
			continue
		}

		start := i
		for i < len(path) && path[i] != '.' && path[i] != '[' {
			i++
		}
		segment := pathSegment{Name: strings.TrimSpace(path[start:i])}
		if segment.Name == "" {
			return nil, fmt.Errorf("invalid path %q", path)
		}

		for i < len(path) && path[i] == '[' {
			end := findClosingBracket(path, i)
			if end < 0 {
				return nil, fmt.Errorf("invalid path index in %q", path)
			}
			segment.Indexes = append(segment.Indexes, strings.TrimSpace(path[i+1:end]))
			i = end + 1
		}

		segments = append(segments, segment)
	}
	return segments, nil
}

// findClosingBracket 查找路径索引表达式的右方括号。
//
// 查找时会跳过引号里的 `]`，让 `map['a]b']` 这类 key 能被正确识别。
func findClosingBracket(path string, start int) int {
	var quote byte
	for i := start + 1; i < len(path); i++ {
		c := path[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(path) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == ']' {
			return i
		}
	}
	return -1
}

// applyIndexes 依次应用路径段上的索引。
//
// 一个路径段允许出现多个索引，例如 `matrix[0][1]`，这里会逐层取值。
func applyIndexes(value any, indexes []string, s *scope) (any, error) {
	current := value
	for _, index := range indexes {
		resolvedIndex, err := resolveIndexValue(index, s)
		if err != nil {
			return nil, err
		}
		next, ok := resolveIndex(current, resolvedIndex)
		if !ok {
			return nil, fmt.Errorf("cannot index %T with %v", current, resolvedIndex)
		}
		current = next
	}
	return current, nil
}

// resolveIndexValue 解析方括号里的索引值。
//
// 支持字符串字面量、数字字面量和作用域变量；其它内容会按原始字符串作为 map key 使用。
func resolveIndexValue(raw string, s *scope) (any, error) {
	if unquoted, ok := unquoteLiteral(raw); ok {
		return unquoted, nil
	}
	if i, err := strconv.Atoi(raw); err == nil {
		return i, nil
	}
	if value, ok := s.lookup(raw); ok {
		return value, nil
	}
	return raw, nil
}

// resolveIndex 对 slice、array、string 或 map 执行单次索引读取。
//
// map 索引会先尝试精确类型匹配，失败后再用字符串形式兜底，以兼容模板里的字符串 key。
func resolveIndex(value any, index any) (any, bool) {
	rv := reflect.ValueOf(value)
	for rv.IsValid() && (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil, false
	}

	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.String:
		i, ok := asInt(index)
		if !ok || i < 0 {
			return nil, false
		}
		if rv.Kind() == reflect.String {
			runes := []rune(rv.String())
			if i >= len(runes) {
				return nil, false
			}
			return string(runes[i]), true
		}
		if i >= rv.Len() {
			return nil, false
		}
		return rv.Index(i).Interface(), true
	case reflect.Map:
		key := reflect.ValueOf(index)
		if key.IsValid() && key.Type().AssignableTo(rv.Type().Key()) {
			value := rv.MapIndex(key)
			if value.IsValid() {
				return value.Interface(), true
			}
		}
		for _, mapKey := range rv.MapKeys() {
			if fmt.Sprint(mapKey.Interface()) == fmt.Sprint(index) {
				value := rv.MapIndex(mapKey)
				if value.IsValid() {
					return value.Interface(), true
				}
			}
		}
	}
	return nil, false
}

// resolveProperty 从 map、结构体、方法或集合长度中读取属性。
//
// 结构体支持导出字段、json tag 和无参 getter；slice/array/string 支持 size 和 length。
func resolveProperty(value any, name string) (any, bool) {
	if name == "" {
		return value, true
	}

	rv := reflect.ValueOf(value)
	for rv.IsValid() && (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil, false
	}

	switch rv.Kind() {
	case reflect.Map:
		return resolveMapProperty(rv, name)
	case reflect.Struct:
		if v, ok := resolveStructField(rv, name); ok {
			return v, true
		}
		return resolveStructMethod(rv, name)
	case reflect.Slice, reflect.Array, reflect.String:
		switch strings.ToLower(name) {
		case "size", "length":
			return rv.Len(), true
		}
	}
	return nil, false
}

// resolveMapProperty 从 map 中读取属性。
//
// 字符串 key 会优先精确匹配；其它 key 类型或大小写不一致时，会用字符串形式做宽松匹配。
func resolveMapProperty(rv reflect.Value, name string) (any, bool) {
	if rv.Type().Key().Kind() == reflect.String {
		key := reflect.ValueOf(name).Convert(rv.Type().Key())
		value := rv.MapIndex(key)
		if value.IsValid() {
			return value.Interface(), true
		}
	}
	for _, mapKey := range rv.MapKeys() {
		if strings.EqualFold(fmt.Sprint(mapKey.Interface()), name) {
			value := rv.MapIndex(mapKey)
			if value.IsValid() {
				return value.Interface(), true
			}
		}
	}
	return nil, false
}

// resolveStructField 从结构体导出字段中读取属性。
//
// 匹配顺序包含字段名、字段名大小写不敏感匹配，以及 json tag 名称。
func resolveStructField(rv reflect.Value, name string) (any, bool) {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" {
			continue
		}
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if field.Name == name || strings.EqualFold(field.Name, name) || jsonName == name {
			return rv.Field(i).Interface(), true
		}
	}
	return nil, false
}

// resolveStructMethod 从结构体无参方法中读取属性。
//
// 支持 `name`、`GetName`、`IsName` 三种方法命名，方法必须无入参且只有一个返回值。
func resolveStructMethod(rv reflect.Value, name string) (any, bool) {
	candidates := []string{name, "Get" + upperFirst(name), "Is" + upperFirst(name)}
	for _, candidate := range candidates {
		method := rv.MethodByName(candidate)
		if !method.IsValid() {
			method = reflect.ValueOf(rv.Interface()).MethodByName(candidate)
		}
		if method.IsValid() && method.Type().NumIn() == 0 && method.Type().NumOut() == 1 {
			return method.Call(nil)[0].Interface(), true
		}
	}
	return nil, false
}

// evalMessageExpression 计算 #{...} 消息表达式。
//
// 支持 `#{key}` 和 `#{key(${param})}`。消息模板中的 `{0}`、`{1}` 会按参数顺序替换。
// 如果 Context.Messages 中没有对应 key，则返回 key 本身，便于开发阶段发现缺失文案。
func (e *Engine) evalMessageExpression(raw string, s *scope) (any, error) {
	keyExpr, paramExprs := splitCall(strings.TrimSpace(raw))
	key, err := e.evalMessageKey(keyExpr, s)
	if err != nil {
		return "", err
	}

	message, ok := s.ctx.Messages[key]
	if !ok {
		return key, nil
	}

	for i, paramExpr := range paramExprs {
		value, err := e.evalAny(paramExpr, s)
		if err != nil {
			return "", err
		}
		message = strings.ReplaceAll(message, "{"+strconv.Itoa(i)+"}", valueToString(value))
	}
	return message, nil
}

// evalMessageKey 解析消息 key。
//
// key 可以是普通字符串、带引号的字面量，也可以是一个完整表达式，例如 `#{${messageKey}}`。
func (e *Engine) evalMessageKey(raw string, s *scope) (string, error) {
	raw = strings.TrimSpace(raw)
	if unquoted, ok := unquoteLiteral(raw); ok {
		return unquoted, nil
	}
	if token, ok := readExpressionAt(raw, 0); ok && token.End == len(raw) {
		value, err := e.evalExpression(token.Kind, token.Body, s)
		if err != nil {
			return "", err
		}
		return valueToString(value), nil
	}
	return raw, nil
}

// evalURLExpression 计算 @{...} 链接表达式。
//
// 支持路径变量和查询参数，例如 `@{/orders/{id}(id=${order.id},token=${token})}`。
// 如果 Context.BaseURL 不为空且路径以 `/` 开头，会自动拼接为绝对地址。
func (e *Engine) evalURLExpression(raw string, s *scope) (any, error) {
	pathExpr, paramsExpr := splitURLParams(strings.TrimSpace(raw))
	if unquoted, ok := unquoteLiteral(pathExpr); ok {
		pathExpr = unquoted
	}
	pathValue, err := e.evalInlineString(pathExpr, s)
	if err != nil {
		return "", err
	}

	params, err := e.evalURLParams(paramsExpr, s)
	if err != nil {
		return "", err
	}

	usedParams := map[string]bool{}
	for key, value := range params {
		placeholder := "{" + key + "}"
		if strings.Contains(pathValue, placeholder) {
			pathValue = strings.ReplaceAll(pathValue, placeholder, url.PathEscape(valueToString(value)))
			usedParams[key] = true
		}
	}

	if s.ctx.BaseURL != "" && strings.HasPrefix(pathValue, "/") {
		pathValue = strings.TrimRight(s.ctx.BaseURL, "/") + pathValue
	}

	u, err := url.Parse(pathValue)
	if err != nil {
		return "", err
	}
	query := u.Query()
	for key, value := range params {
		if usedParams[key] {
			continue
		}
		query.Set(key, valueToString(value))
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// evalURLParams 解析链接表达式括号中的参数。
//
// 参数格式为 `name=expr`，多个参数用逗号分隔；每个参数值都会再次走 evalAny，
// 因此可以使用变量表达式、字符串字面量或混合表达式。
func (e *Engine) evalURLParams(raw string, s *scope) (map[string]any, error) {
	params := map[string]any{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return params, nil
	}

	for _, part := range splitTopLevel(raw, ',') {
		key, valueExpr, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("invalid URL parameter %q", part)
		}
		key = strings.TrimSpace(key)
		value, err := e.evalAny(strings.TrimSpace(valueExpr), s)
		if err != nil {
			return nil, err
		}
		params[key] = value
	}
	return params, nil
}

// evalFragmentExpression 计算 ~{...} 片段表达式。
//
// 当前实现从 Context.Fragments 中按 key 读取 HTML 片段。这个设计适合邮件模板复用页脚、
// 公司签名、退订说明等固定片段。
func (e *Engine) evalFragmentExpression(raw string, s *scope) (any, error) {
	key := strings.TrimSpace(raw)
	if unquoted, ok := unquoteLiteral(key); ok {
		key = unquoted
	}
	key, err := e.evalInlineString(key, s)
	if err != nil {
		return "", err
	}
	if fragment, ok := s.ctx.Fragments[key]; ok {
		return fragment, nil
	}
	return "", fmt.Errorf("unknown fragment %q", key)
}

// splitTernary 拆分顶层三元表达式。
//
// 只识别顶层的 `?` 与 `:`，不会把引号、括号或花括号内部的符号误判为三元表达式。
func splitTernary(raw string) (string, string, string, bool) {
	question := findTopLevelByte(raw, '?')
	if question < 0 {
		return "", "", "", false
	}
	colon := findTopLevelByte(raw[question+1:], ':')
	if colon < 0 {
		return "", "", "", false
	}
	colon += question + 1
	return strings.TrimSpace(raw[:question]), strings.TrimSpace(raw[question+1 : colon]), strings.TrimSpace(raw[colon+1:]), true
}

// splitComparison 拆分顶层比较表达式。
//
// operator 由调用方按优先顺序传入，例如先匹配 `>=` 再匹配 `>`，避免长操作符被截断。
func splitComparison(raw string, operator string) (string, string, bool) {
	index := findTopLevelString(raw, operator)
	if index < 0 {
		return "", "", false
	}
	return strings.TrimSpace(raw[:index]), strings.TrimSpace(raw[index+len(operator):]), true
}

// splitURLParams 把 @{...} 拆成路径部分和参数部分。
//
// 例如 `/orders/{id}(id=${id})` 会拆成 `/orders/{id}` 与 `id=${id}`。
// 只识别顶层括号，避免路径变量里的花括号影响判断。
func splitURLParams(raw string) (string, string) {
	var quote byte
	braceDepth := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(raw) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		switch c {
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '(':
			if braceDepth == 0 && strings.HasSuffix(strings.TrimSpace(raw[i:]), ")") {
				tail := strings.TrimSpace(raw[i+1:])
				return strings.TrimSpace(raw[:i]), strings.TrimSpace(tail[:len(tail)-1])
			}
		}
	}
	return raw, ""
}

// splitTopLevelOperator 按顶层操作符拆分表达式。
//
// 只拆分不在引号、括号、方括号、花括号内部的操作符，适合处理 `&&`、`||` 这类符号。
func splitTopLevelOperator(raw string, operator string) []string {
	var out []string
	start := 0
	for {
		index := findTopLevelString(raw[start:], operator)
		if index < 0 {
			break
		}
		index += start
		out = append(out, strings.TrimSpace(raw[start:index]))
		start = index + len(operator)
	}
	if len(out) == 0 {
		return []string{strings.TrimSpace(raw)}
	}
	out = append(out, strings.TrimSpace(raw[start:]))
	return out
}

// splitTopLevelWord 按顶层关键字拆分表达式。
//
// 用于处理 `and`、`or` 这类文字操作符，并通过 isIdentifierRune 避免把变量名里的片段误判
// 成关键字，例如 `order` 不会被拆成 `or`。
func splitTopLevelWord(raw string, word string) []string {
	var out []string
	start := 0
	for {
		index := findTopLevelWord(raw[start:], word)
		if index < 0 {
			break
		}
		index += start
		out = append(out, strings.TrimSpace(raw[start:index]))
		start = index + len(word)
	}
	if len(out) == 0 {
		return []string{strings.TrimSpace(raw)}
	}
	out = append(out, strings.TrimSpace(raw[start:]))
	return out
}

// findTopLevelByte 查找顶层单字符符号的位置。
//
// 顶层表示当前位置不在引号或任何括号结构内部。
func findTopLevelByte(raw string, target byte) int {
	return findTopLevel(raw, func(raw string, i int) int {
		if raw[i] == target {
			return 1
		}
		return 0
	})
}

// findTopLevelString 查找顶层字符串符号的位置。
//
// 主要用于比较操作符和逻辑操作符的定位。
func findTopLevelString(raw string, target string) int {
	return findTopLevel(raw, func(raw string, i int) int {
		if strings.HasPrefix(raw[i:], target) {
			return len(target)
		}
		return 0
	})
}

// findTopLevelWord 查找顶层关键字的位置。
//
// 只有关键字两侧都不是标识符字符时才算匹配，避免误伤变量名或属性名。
func findTopLevelWord(raw string, word string) int {
	return findTopLevel(raw, func(raw string, i int) int {
		if !strings.HasPrefix(raw[i:], word) {
			return 0
		}
		beforeOK := i == 0 || !isIdentifierRune(rune(raw[i-1]))
		afterIndex := i + len(word)
		afterOK := afterIndex >= len(raw) || !isIdentifierRune(rune(raw[afterIndex]))
		if beforeOK && afterOK {
			return len(word)
		}
		return 0
	})
}

// findTopLevel 是顶层查找的通用实现。
//
// 它维护引号状态和括号深度，调用方只需要提供当前位置是否命中的判断函数。
func findTopLevel(raw string, match func(raw string, i int) int) int {
	var quote byte
	depth := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(raw) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && match(raw, i) > 0 {
				return i
			}
		}
	}
	return -1
}

// splitCall 拆分函数式调用表达式。
//
// 例如 `mail.hello(${name})` 会拆成 `mail.hello` 和参数列表。当前主要服务于消息表达式。
func splitCall(raw string) (string, []string) {
	raw = strings.TrimSpace(raw)
	if !strings.HasSuffix(raw, ")") {
		return raw, nil
	}
	start := strings.IndexByte(raw, '(')
	if start < 0 {
		return raw, nil
	}
	return strings.TrimSpace(raw[:start]), splitTopLevel(raw[start+1:len(raw)-1], ',')
}

// splitTopLevel 按顶层分隔符拆分字符串。
//
// 常用于逗号参数列表和加号拼接表达式，确保引号或括号内部的分隔符不会被拆开。
func splitTopLevel(raw string, sep byte) []string {
	var out []string
	var quote byte
	depth := 0
	start := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(raw) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		default:
			if c == sep && depth == 0 {
				out = append(out, strings.TrimSpace(raw[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(raw[start:]))
	return out
}

// compareValues 根据指定比较操作符比较两个值。
//
// 两侧都能转成数字时按数值比较，否则按字符串比较；这种策略足够覆盖邮件模板里的状态判断。
func compareValues(left any, right any, operator string) bool {
	if leftFloat, leftOK := toFloat64(left); leftOK {
		if rightFloat, rightOK := toFloat64(right); rightOK {
			switch operator {
			case "==":
				return leftFloat == rightFloat
			case "!=":
				return leftFloat != rightFloat
			case ">":
				return leftFloat > rightFloat
			case ">=":
				return leftFloat >= rightFloat
			case "<":
				return leftFloat < rightFloat
			case "<=":
				return leftFloat <= rightFloat
			}
		}
	}

	leftString := valueToString(left)
	rightString := valueToString(right)
	switch operator {
	case "==":
		return leftString == rightString
	case "!=":
		return leftString != rightString
	case ">":
		return leftString > rightString
	case ">=":
		return leftString >= rightString
	case "<":
		return leftString < rightString
	case "<=":
		return leftString <= rightString
	default:
		return false
	}
}

// toFloat64 尝试把常见数字类型或数字字符串转换为 float64。
//
// 比较表达式会用它判断是否可以进行数值比较。
func toFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// isTruthy 把任意值转换为条件判断中的真假值。
//
// 规则面向模板使用习惯：nil、false、0、空字符串、"false"、"0"、空集合都为假，
// 其它值为真。
func isTruthy(value any) bool {
	if value == nil {
		return false
	}

	rv := reflect.ValueOf(value)
	for rv.IsValid() && (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return false
	}

	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool()
	case reflect.String:
		value := strings.TrimSpace(strings.ToLower(rv.String()))
		return value != "" && value != "false" && value != "0" && value != "no" && value != "off"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() != 0
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	default:
		return true
	}
}

// isIdentifierRune 判断字符是否属于表达式标识符的一部分。
//
// 点号也视为标识符字符，是为了让 `user.name` 在关键字查找时被当成连续路径。
func isIdentifierRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.'
}

// unquoteLiteral 解析字符串字面量。
//
// 支持单引号和双引号，并处理常见转义字符；返回值中的 bool 表示输入是否确实是字符串字面量。
func unquoteLiteral(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return "", false
	}
	first, last := raw[0], raw[len(raw)-1]
	if first != last || (first != '"' && first != '\'') {
		return "", false
	}
	body := raw[1 : len(raw)-1]
	body = strings.ReplaceAll(body, `\\`, `\`)
	body = strings.ReplaceAll(body, `\"`, `"`)
	body = strings.ReplaceAll(body, `\'`, `'`)
	return body, true
}

// asInt 尝试把值转换为 int。
//
// 索引读取 slice、array、string 时需要整数下标，因此这里兼容常见整数类型和数字字符串。
func asInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case string:
		i, err := strconv.Atoi(v)
		return i, err == nil
	default:
		return 0, false
	}
}

// upperFirst 把字符串首字母转为大写。
//
// 结构体 getter 匹配会用它从 `name` 推导出 `GetName` 或 `IsName`。
func upperFirst(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// valueToString 把任意渲染结果转换为字符串。
//
// nil 会输出为空字符串；实现 fmt.Stringer 的值优先使用 String 方法。
func valueToString(value any) string {
	if value == nil {
		return ""
	}
	if stringer, ok := value.(fmt.Stringer); ok {
		return stringer.String()
	}
	return fmt.Sprint(value)
}
