package thymeleaf

import "testing"

type testUser struct {
	ID    int
	Name  string `json:"name"`
	Email string
}

func TestTextAndUText(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"name": "<b>Tom</b>",
	})

	got, err := engine.ProcessStringContext(`<p th:text="${name}">fallback</p><div th:utext="${name}">fallback</div>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<p>&lt;b&gt;Tom&lt;/b&gt;</p><div><b>Tom</b></div>`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}

func TestObjectAndValue(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"user": testUser{ID: 7, Name: "Alice", Email: "alice@example.com"},
	})

	got, err := engine.ProcessStringContext(`<form th:object="${user}"><input th:id="*{ID}" th:value="*{name}" /></form>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<form><input id="7" value="Alice"></form>`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}

func TestEachAndStatus(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"items": []string{"Piano", "Guitar"},
	})

	got, err := engine.ProcessStringContext(`<ul><li th:each="item, stat : ${items}" th:text="${stat.count + '. ' + item}">demo</li></ul>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<ul><li>1. Piano</li><li>2. Guitar</li></ul>`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}

func TestHrefSrcAndURLExpression(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"order": map[string]any{"id": 1001},
		"token": "a b",
	})
	ctx.BaseURL = "https://example.com"

	got, err := engine.ProcessStringContext(`<a th:href="@{/orders/{id}(id=${order.id},token=${token})}">view</a><img th:src="@{/assets/logo.png}" />`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<a href="https://example.com/orders/1001?token=a+b">view</a><img src="https://example.com/assets/logo.png">`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}

func TestIfUnless(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"show":   true,
		"hidden": false,
		"paid":   false,
		"count":  2,
	})

	got, err := engine.ProcessStringContext(`<p th:if="${show}">show</p><p th:if="${hidden}">hidden</p><p th:unless="${paid}">unpaid</p><p th:unless="${count > 1}">single</p><strong th:if="${show} and ${count} > 1">complex</strong><span th:if="not ${hidden}">not-hidden</span>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<p>show</p><p>unpaid</p><strong>complex</strong><span>not-hidden</span>`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}

func TestClassAppend(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"active": true,
		"kind":   "pill",
		"empty":  "",
	})

	got, err := engine.ProcessStringContext(`<div class="card" th:classappend="${active ? 'active' : ''}"></div><em class="card" th:classappend="${active} ? 'hot' : ''"></em><span th:classappend="${kind}"></span><p class="base" th:classappend="${empty}"></p>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<div class="card active"></div><em class="card hot"></em><span class="pill"></span><p class="base"></p>`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}

func TestEachWithIfAndClassAppend(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"items": []map[string]any{
			{"name": "Piano", "visible": true},
			{"name": "Hidden", "visible": false},
			{"name": "Guitar", "visible": true},
		},
	})

	got, err := engine.ProcessStringContext(`<ul><li class="row" th:each="item : ${items}" th:if="${item.visible}" th:classappend="${itemStat.odd ? 'odd' : 'even'}" th:text="${item.name}">demo</li></ul>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<ul><li class="row even">Piano</li><li class="row even">Guitar</li></ul>`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}

func TestMessageAndFragmentExpressions(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"user": testUser{Name: "Alice"},
	})
	ctx.Messages = map[string]string{
		"mail.hello": "Hello, {0}",
	}
	ctx.Fragments = map[string]string{
		"mail/footer :: signature": "<footer>DaDa</footer>",
	}

	got, err := engine.ProcessStringContext(`<p th:text="#{mail.hello(${user.name})}">hello</p><section th:utext="~{mail/footer :: signature}"></section>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<p>Hello, Alice</p><section><footer>DaDa</footer></section>`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}

func TestTemplateCanKeepBrowserPreviewFallback(t *testing.T) {
	engine := New()
	ctx := NewContext(map[string]any{
		"title": "Welcome",
	})

	got, err := engine.ProcessStringContext(`<html xmlns:th="http://www.thymeleaf.org"><body><h1 th:text="${title}">Browser Preview Title</h1></body></html>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := `<html><body><h1>Welcome</h1></body></html>`
	if got != want {
		t.Fatalf("unexpected render:\nwant: %s\n got: %s", want, got)
	}
}
