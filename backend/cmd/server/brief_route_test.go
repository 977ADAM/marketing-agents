package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/core/limits"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	brief "github.com/977ADAM/marketing-agents/internal/features/brief/domain"
	briefhttp "github.com/977ADAM/marketing-agents/internal/features/brief/transport/http"
)

// fakeInterviewService — подмена сервиса интервью: в этих тестах запрос
// отсекается лимитом тела до вызова модели, но интерфейс обязывает
// реализовать Ask.
type fakeInterviewService struct{}

func (fakeInterviewService) Ask(context.Context, []brief.Message, brief.Draft, func(string)) (brief.Result, corellm.Usage, error) {
	return brief.Result{}, corellm.Usage{}, nil
}

// TestBriefRouteEnforcesConfiguredLimits проверяет, что лимиты, переданные в
// NewHandler при регистрации маршрута, действительно ограничивают тело
// запроса на эндпоинте: тело больше нестандартного лимита получает 413, а не
// разбирается по дефолтным 2 МиБ из limits.Defaults().
func TestBriefRouteEnforcesConfiguredLimits(t *testing.T) {
	api := server.New()
	api.RegisterRoutes(briefhttp.NewHandler(
		fakeInterviewService{},
		middleware.NewRateLimiter(0),
		limits.Limits{MaxJSONBytes: 64},
	).Routes()...)

	// Тело заведомо больше лимита в 64 байта.
	body := `{"messages":[{"role":"user","text":"` + strings.Repeat("я", 256) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/briefs/interview", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("статус = %d, want %d (тело %d байт, лимит 64)", rec.Code, http.StatusRequestEntityTooLarge, len(body))
	}
}

// TestBriefRoutePassesConfiguredLimits проверяет проводку композиционного
// корня: в main.go маршрут интервью обязан получать cfg.Limits — как соседние
// маршруты кампаний и проверок, — иначе MAX_JSON_BYTES из конфигурации молча
// не действует на этот эндпоинт.
//
// Поведенчески эту проводку не проверить: main() поднимает HTTP-сервер и
// требует готовую схему БД, поэтому тест разбирает исходник main.go и смотрит
// аргументы вызова briefhttp.NewHandler, зарегистрированного через
// api.RegisterRoutes. Тест падает, если третий аргумент потерян, заменён на
// дефолтные лимиты или обработчик перестал регистрироваться.
func TestBriefRoutePassesConfiguredLimits(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("разбор main.go: %v", err)
	}

	var handlers []*ast.CallExpr
	registered := map[*ast.CallExpr]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isSelectorCall(call.Fun, "briefhttp", "NewHandler") {
			handlers = append(handlers, call)
			return true
		}
		if !isSelectorCall(call.Fun, "api", "RegisterRoutes") {
			return true
		}
		// Обработчик должен быть не просто собран, а зарегистрирован.
		for _, arg := range call.Args {
			ast.Inspect(arg, func(n ast.Node) bool {
				if inner, ok := n.(*ast.CallExpr); ok && isSelectorCall(inner.Fun, "briefhttp", "NewHandler") {
					registered[inner] = true
				}
				return true
			})
		}
		return true
	})

	if len(handlers) != 1 {
		t.Fatalf("вызовов briefhttp.NewHandler в main.go: %d, want 1", len(handlers))
	}
	handler := handlers[0]
	if !registered[handler] {
		t.Fatal("обработчик интервью собран, но не зарегистрирован через api.RegisterRoutes")
	}
	if len(handler.Args) != 3 {
		t.Fatalf("аргументов briefhttp.NewHandler: %d, want 3 (сервис, лимитер, лимиты)", len(handler.Args))
	}
	if got := exprString(fset, handler.Args[2]); got != "cfg.Limits" {
		t.Errorf("третий аргумент briefhttp.NewHandler = %q, want %q: лимиты конфигурации не дойдут до эндпоинта", got, "cfg.Limits")
	}
}

// isSelectorCall сообщает, что выражение — вызов pkg.name(...).
func isSelectorCall(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg
}

// exprString печатает выражение так, как оно выглядит в исходнике.
func exprString(fset *token.FileSet, expr ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, expr); err != nil {
		return ""
	}
	return b.String()
}
