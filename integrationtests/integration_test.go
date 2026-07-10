package integrationtests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/gofiber/fiber/v2"
	godichi "github.com/junioryono/godi/chi/v5"
	godiecho "github.com/junioryono/godi/echo/v5"
	godifiber "github.com/junioryono/godi/fiber/v5"
	godigin "github.com/junioryono/godi/gin/v5"
	godihttp "github.com/junioryono/godi/http/v5"
	godihuma "github.com/junioryono/godi/huma/v5"
	"github.com/junioryono/godi/v5"
	"github.com/labstack/echo/v4"
)

type requestResource struct {
	closeCalls atomic.Int32
}

func (r *requestResource) Close() error {
	r.closeCalls.Add(1)
	return nil
}

type greetingController struct {
	resource *requestResource
}

type greetingInput struct {
	Name string `path:"name"`
}

type greetingOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

func (c *greetingController) Greet(_ context.Context, input *greetingInput) (*greetingOutput, error) {
	output := &greetingOutput{}
	output.Body.Message = "hello " + input.Name
	return output, nil
}

type compositionRunner func(*testing.T, godi.Provider) (int, string)

func buildRequestProvider(t *testing.T) (provider godi.Provider, getResource func() *requestResource) {
	t.Helper()

	var resource *requestResource
	services := godi.NewCollection()
	services.AddScoped(func() *requestResource {
		resource = &requestResource{}
		return resource
	})
	services.AddScoped(func(r *requestResource) *greetingController {
		return &greetingController{resource: r}
	})

	provider, err := services.Build()
	if err != nil {
		t.Fatal(err)
	}
	return provider, func() *requestResource { return resource }
}

func registerGreeting(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "greet",
		Method:      http.MethodGet,
		Path:        "/greet/{name}",
	}, godihuma.Handle((*greetingController).Greet))
}

func runNetHTTP(t *testing.T, provider godi.Provider) (status int, body string) {
	t.Helper()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Integration", "1.0.0"))
	registerGreeting(api)

	handler := godihttp.ScopeMiddleware(provider)(mux)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/greet/world", http.NoBody))
	return recorder.Code, recorder.Body.String()
}

func runChi(t *testing.T, provider godi.Provider) (status int, body string) {
	t.Helper()
	router := chi.NewRouter()
	router.Use(godichi.ScopeMiddleware(provider))
	registerGreeting(humachi.New(router, huma.DefaultConfig("Integration", "1.0.0")))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/greet/world", http.NoBody))
	return recorder.Code, recorder.Body.String()
}

func runGin(t *testing.T, provider godi.Provider) (status int, body string) {
	t.Helper()
	engine := gin.New()
	engine.Use(godigin.ScopeMiddleware(provider))
	registerGreeting(humagin.New(engine, huma.DefaultConfig("Integration", "1.0.0")))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/greet/world", http.NoBody))
	return recorder.Code, recorder.Body.String()
}

func runEcho(t *testing.T, provider godi.Provider) (status int, body string) {
	t.Helper()
	engine := echo.New()
	engine.Use(godiecho.ScopeMiddleware(provider))
	registerGreeting(humaecho.New(engine, huma.DefaultConfig("Integration", "1.0.0")))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/greet/world", http.NoBody))
	return recorder.Code, recorder.Body.String()
}

func runFiber(t *testing.T, provider godi.Provider) (status int, body string) {
	t.Helper()
	app := fiber.New()
	app.Use(godifiber.ScopeMiddleware(provider))
	registerGreeting(humafiber.New(app, huma.DefaultConfig("Integration", "1.0.0")))

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/greet/world", http.NoBody), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(responseBody)
}

func TestHumaRouterCompositions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		run  compositionRunner
	}{
		{name: "net/http", run: runNetHTTP},
		{name: "chi", run: runChi},
		{name: "gin", run: runGin},
		{name: "echo", run: runEcho},
		{name: "fiber", run: runFiber},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, getResource := buildRequestProvider(t)
			defer provider.Close()

			status, body := test.run(t, provider)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", status, http.StatusOK, body)
			}
			if !strings.Contains(body, "hello world") {
				t.Fatalf("response body does not contain greeting: %s", body)
			}

			resource := getResource()
			if resource == nil {
				t.Fatal("scoped resource was not constructed")
			}
			if calls := resource.closeCalls.Load(); calls != 1 {
				t.Fatalf("resource Close calls = %d, want 1", calls)
			}
		})
	}
}
