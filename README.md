# thymeleaf-go

`thymeleaf-go` 是一个用 Go 编写的轻量 Thymeleaf 风格模板组件，目标场景是 HTML 邮件模板。

它保留 Thymeleaf 对邮件模板很有价值的特性：模板文件本身仍然是可以直接用浏览器预览的 HTML，而服务端渲染时会读取 `th:*` 属性并替换为真实业务数据。

## 当前支持范围

标签属性：

- `th:id`
- `th:text`
- `th:utext`
- `th:object`
- `th:value`
- `th:each`
- `th:href`
- `th:src`
- `th:if`
- `th:unless`
- `th:classappend`

表达式：

- `${...}`：变量表达式
- `*{...}`：选择对象表达式，通常配合 `th:object`
- `#{...}`：消息表达式
- `@{...}`：链接表达式，支持路径变量和查询参数
- `~{...}`：片段表达式

## 快速示例

```go
package main

import (
	"fmt"

	"github.com/xlizy/thymeleaf-go"
)

func main() {
	engine := thymeleaf.New()

	ctx := thymeleaf.NewContext(map[string]any{
		"user": map[string]any{
			"id":   1001,
			"name": "Alice",
		},
		"items": []string{"钢琴", "吉他"},
	})
	ctx.BaseURL = "https://www.example.com"
	ctx.Messages = map[string]string{
		"mail.hello": "你好，{0}",
	}

	html, err := engine.ProcessStringContext(`
		<html xmlns:th="http://www.thymeleaf.org">
		<body>
			<h1 th:text="#{mail.hello(${user.name})}">浏览器预览标题</h1>
			<a th:href="@{/users/{id}(id=${user.id})}">查看用户</a>
			<p th:if="${user.id > 0}" class="notice" th:classappend="${user.id == 1001 ? 'notice-primary' : ''}">用户有效</p>
			<ul>
				<li th:each="item, stat : ${items}" th:text="${stat.count + '. ' + item}">预览列表项</li>
			</ul>
		</body>
		</html>
	`, ctx)
	if err != nil {
		panic(err)
	}

	fmt.Println(html)
}
```

## 设计边界

这是 Thymeleaf 的 Go 子集实现，不是 Java `org.thymeleaf` 的完整移植。当前重点是邮件模板常用能力：

- `th:text` 会做 HTML 转义，适合普通文本。
- `th:utext` 不做 HTML 转义，适合已经可信的富文本片段。
- `th:each` 会重复当前元素本身，并提供 `index/count/size/current/even/odd/first/last` 状态字段。
- `th:if` 与 `th:unless` 会控制当前元素是否输出，支持布尔值、数字、字符串、集合长度等常见真假判断。
- `th:classappend` 会在原有 `class` 后追加表达式结果，表达式为空时不会追加多余空格。
- 条件表达式支持常见写法，例如 `${active ? 'on' : ''}`、`${active} ? 'on' : ''`、`${count} > 1`、`${a} and not ${b}`。
- `@{...}` 支持 `@{/orders/{id}(id=${order.id},token=${token})}` 这类链接。
- `~{...}` 当前从 `Context.Fragments` 中按 key 获取 HTML 片段。

后续如果邮件模板需要更多 Thymeleaf 语法，可以继续扩展内联文本表达式、布局片段、`th:remove`、`th:with` 等能力。
