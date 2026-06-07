package thymeleaf

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Entry 是 th:each 遍历 map 时暴露给模板的条目值。
//
// 这样模板可以通过 `${entry.key}` 与 `${entry.value}` 读取 map 的键和值，而不是依赖
// Go 反射里的 map 结构细节。
type Entry struct {
	Key   any
	Value any
}

// IterationStatus 是 th:each 的迭代状态对象。
//
// 字段命名对齐 Thymeleaf 常用状态变量，模板可以使用 `${itemStat.count}`、
// `${itemStat.first}`、`${itemStat.last}` 等表达式。
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

// eachSpec 是解析后的 th:each 配置。
//
// itemVar 是当前项变量名，statusVar 是状态变量名，expr 是集合表达式。
type eachSpec struct {
	itemVar   string
	statusVar string
	expr      string
}

// renderEach 渲染 th:each 元素。
//
// Thymeleaf 的 th:each 会重复当前元素本身，而不是只重复子节点；因此每个元素项都会重新
// 调用 renderElement，并在局部 scope 中放入 item 变量和状态变量。
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

// parseEachSpec 解析 th:each 属性值。
//
// 支持 `item : ${items}` 和 `item, stat : ${items}` 两种写法；未显式声明状态变量时，
// 默认使用 `itemStat`，这与 Thymeleaf 的默认命名习惯保持一致。
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

// iterationItems 把可迭代值转换成 []any。
//
// 当前支持 slice、array 和 map。map 会按 key 的字符串形式排序，保证渲染结果稳定，
// 避免 Go map 随机遍历顺序导致邮件内容或测试结果抖动。
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
