package thymeleaf

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Entry is the value exposed when th:each iterates over a map.
//
// It lets templates use expressions such as ${entry.key} and ${entry.value}.
type Entry struct {
	Key   any
	Value any
}

// IterationStatus mirrors the commonly used Thymeleaf iteration status fields.
type IterationStatus struct {
	Index   int
	Count   int
	Size    int
	Current any
	Even    bool
	Odd     bool
	First   bool
	Last    bool
}

type eachSpec struct {
	itemVar   string
	statusVar string
	expr      string
}

func (e *Engine) renderEach(n *node, s *scope, rawSpec string) (string, error) {
	spec, err := parseEachSpec(rawSpec)
	if err != nil {
		return "", err
	}

	collectionValue, err := e.evalAny(spec.expr, s)
	if err != nil {
		return "", fmt.Errorf("render th:each collection %q: %w", spec.expr, err)
	}

	items, err := iterationItems(collectionValue)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	size := len(items)
	for i, item := range items {
		status := IterationStatus{
			Index:   i,
			Count:   i + 1,
			Size:    size,
			Current: item,
			Even:    i%2 == 0,
			Odd:     i%2 != 0,
			First:   i == 0,
			Last:    i == size-1,
		}

		itemScope := s.withVar(spec.itemVar, item).withVar(spec.statusVar, status)
		rendered, err := e.renderElement(n, itemScope)
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
	}
	return b.String(), nil
}

func parseEachSpec(raw string) (eachSpec, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return eachSpec{}, fmt.Errorf("invalid th:each %q, expected \"item : ${items}\"", raw)
	}

	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	if left == "" || right == "" {
		return eachSpec{}, fmt.Errorf("invalid th:each %q", raw)
	}

	names := strings.Split(left, ",")
	itemVar := strings.TrimSpace(names[0])
	if itemVar == "" {
		return eachSpec{}, fmt.Errorf("invalid th:each item variable in %q", raw)
	}

	statusVar := itemVar + "Stat"
	if len(names) > 1 {
		statusVar = strings.TrimSpace(names[1])
		if statusVar == "" {
			return eachSpec{}, fmt.Errorf("invalid th:each status variable in %q", raw)
		}
	}

	return eachSpec{
		itemVar:   itemVar,
		statusVar: statusVar,
		expr:      right,
	}, nil
}

func iterationItems(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, rv.Index(i).Interface())
		}
		return out, nil
	case reflect.Map:
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool {
			return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
		})

		out := make([]any, 0, len(keys))
		for _, key := range keys {
			out = append(out, Entry{
				Key:   key.Interface(),
				Value: rv.MapIndex(key).Interface(),
			})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("th:each expects slice, array, or map, got %T", value)
	}
}
