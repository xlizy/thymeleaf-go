package thymeleaf

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

type exprToken struct {
	Kind byte
	Body string
	End  int
}

type pathSegment struct {
	Name    string
	Indexes []string
}

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

func (e *Engine) evalString(raw string, s *scope) (string, error) {
	value, err := e.evalAny(raw, s)
	if err != nil {
		return "", err
	}
	return valueToString(value), nil
}

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

func splitComparison(raw string, operator string) (string, string, bool) {
	index := findTopLevelString(raw, operator)
	if index < 0 {
		return "", "", false
	}
	return strings.TrimSpace(raw[:index]), strings.TrimSpace(raw[index+len(operator):]), true
}

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

func findTopLevelByte(raw string, target byte) int {
	return findTopLevel(raw, func(raw string, i int) int {
		if raw[i] == target {
			return 1
		}
		return 0
	})
}

func findTopLevelString(raw string, target string) int {
	return findTopLevel(raw, func(raw string, i int) int {
		if strings.HasPrefix(raw[i:], target) {
			return len(target)
		}
		return 0
	})
}

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

func isIdentifierRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.'
}

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

func upperFirst(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func valueToString(value any) string {
	if value == nil {
		return ""
	}
	if stringer, ok := value.(fmt.Stringer); ok {
		return stringer.String()
	}
	return fmt.Sprint(value)
}
