package integrationtests

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/gofiber/fiber/v3"
	godichi "github.com/junioryono/godi/chi/v6"
	godiecho "github.com/junioryono/godi/echo/v6"
	godifiber "github.com/junioryono/godi/fiber/v6"
	godigin "github.com/junioryono/godi/gin/v6"
	godihttp "github.com/junioryono/godi/http/v6"
	"github.com/junioryono/godi/v6"
	"github.com/labstack/echo/v5"
)

// The conformance suite runs the same behavioral contract against every
// router adapter. Each adapter registers these routes:
//
//	GET /probe    Handle(*conformanceProbe): resolves scoped services
//	GET /panic    Handle with WithPanicRecovery(true); the method panics
//	GET /missing  Handle(*unregisteredController): resolution fails
//
// With conformanceOptions.failingMiddleware, ScopeMiddleware also gets a
// WithMiddleware function that resolves a scoped resource and then fails,
// and a WithMiddlewareErrorHandler that responds 401.

const (
	panicDetail      = "conformance panic: secret=hunter2"
	middlewareDetail = "conformance middleware: token=hunter2"
)

var quietLogger = slog.New(slog.DiscardHandler)

// observations is a singleton that records what handlers saw.
type observations struct {
	mu                  sync.Mutex
	resources           []*requestResource
	sameInstance        []bool
	closedDuringHandler []bool
}

func (o *observations) record(resource *requestResource, sameInstance, closed bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.resources = append(o.resources, resource)
	o.sameInstance = append(o.sameInstance, sameInstance)
	o.closedDuringHandler = append(o.closedDuringHandler, closed)
}

func (o *observations) snapshot() (resources []*requestResource, sameInstance, closedDuringHandler []bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]*requestResource(nil), o.resources...),
		append([]bool(nil), o.sameInstance...),
		append([]bool(nil), o.closedDuringHandler...)
}

// conformanceProbe is a scoped controller with a scoped dependency.
type conformanceProbe struct {
	resource     *requestResource
	observations *observations
}

// serve resolves the scoped resource again from the request context and
// records whether it is the controller's instance and whether it is closed.
func (p *conformanceProbe) serve(ctx context.Context) string {
	scope, err := godi.FromContext(ctx)
	if err != nil {
		return "no scope: " + err.Error()
	}
	resolved, err := godi.Resolve[*requestResource](scope)
	if err != nil {
		return "resolve failed: " + err.Error()
	}
	p.observations.record(p.resource, resolved == p.resource, p.resource.closeCalls.Load() > 0)
	return "ok"
}

// unregisteredController is deliberately absent from the container.
type unregisteredController struct{}

// failingMiddleware resolves a scoped resource, then fails like a rejected
// authentication check.
func failingMiddleware(scope godi.Scope) error {
	if _, err := godi.Resolve[*requestResource](scope); err != nil {
		return err
	}
	return errors.New(middlewareDetail)
}

type conformanceOptions struct {
	failingMiddleware bool
}

type conformanceRunner func(*http.Request) (status int, body string)

type conformanceTarget struct {
	name  string
	build func(t *testing.T, provider godi.Provider, opts conformanceOptions) conformanceRunner
}

func recorderRunner(handler http.Handler) conformanceRunner {
	return func(req *http.Request) (int, string) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code, recorder.Body.String()
	}
}

func testRunner(t *testing.T, do func(*http.Request) (*http.Response, error)) conformanceRunner {
	return func(req *http.Request) (int, string) {
		response, err := do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(body)
	}
}

// netHTTPOptions is shared by net/http and chi (a facade over godihttp).
func netHTTPOptions(opts conformanceOptions) []godihttp.Option {
	options := []godihttp.Option{godihttp.WithLogger(quietLogger)}
	if opts.failingMiddleware {
		options = append(options,
			godihttp.WithMiddleware(func(scope godi.Scope, _ *http.Request) error { return failingMiddleware(scope) }),
			godihttp.WithMiddlewareErrorHandler(func(w http.ResponseWriter, _ *http.Request, _ error) {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			}),
		)
	}
	return options
}

func netHTTPRoutes(register func(pattern string, handler http.Handler)) {
	register("/probe", godihttp.Handle(func(p *conformanceProbe, w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, p.serve(r.Context()))
	}, godihttp.WithHandlerLogger(quietLogger)))
	register("/panic", godihttp.Handle(func(p *conformanceProbe, _ http.ResponseWriter, r *http.Request) {
		p.serve(r.Context())
		panic(panicDetail)
	}, godihttp.WithPanicRecovery(true), godihttp.WithHandlerLogger(quietLogger)))
	register("/missing", godihttp.Handle(func(*unregisteredController, http.ResponseWriter, *http.Request) {},
		godihttp.WithHandlerLogger(quietLogger)))
}

func buildNetHTTP(_ *testing.T, provider godi.Provider, opts conformanceOptions) conformanceRunner {
	mux := http.NewServeMux()
	netHTTPRoutes(func(pattern string, handler http.Handler) { mux.Handle("GET "+pattern, handler) })
	return recorderRunner(godihttp.ScopeMiddleware(provider, netHTTPOptions(opts)...)(mux))
}

func buildChi(_ *testing.T, provider godi.Provider, opts conformanceOptions) conformanceRunner {
	router := chi.NewRouter()
	router.Use(godichi.ScopeMiddleware(provider, netHTTPOptions(opts)...))
	netHTTPRoutes(func(pattern string, handler http.Handler) { router.Method(http.MethodGet, pattern, handler) })
	return recorderRunner(router)
}

func buildGin(_ *testing.T, provider godi.Provider, opts conformanceOptions) conformanceRunner {
	options := []godigin.Option{godigin.WithLogger(quietLogger)}
	if opts.failingMiddleware {
		options = append(options,
			godigin.WithMiddleware(func(scope godi.Scope, _ *gin.Context) error { return failingMiddleware(scope) }),
			godigin.WithMiddlewareErrorHandler(func(c *gin.Context, _ error) { c.AbortWithStatus(http.StatusUnauthorized) }),
		)
	}
	engine := gin.New()
	engine.Use(godigin.ScopeMiddleware(provider, options...))
	engine.GET("/probe", godigin.Handle(func(p *conformanceProbe, c *gin.Context) {
		c.String(http.StatusOK, p.serve(c.Request.Context()))
	}, godigin.WithHandlerLogger(quietLogger)))
	engine.GET("/panic", godigin.Handle(func(p *conformanceProbe, c *gin.Context) {
		p.serve(c.Request.Context())
		panic(panicDetail)
	}, godigin.WithPanicRecovery(true), godigin.WithHandlerLogger(quietLogger)))
	engine.GET("/missing", godigin.Handle(func(*unregisteredController, *gin.Context) {},
		godigin.WithHandlerLogger(quietLogger)))
	return recorderRunner(engine)
}

func buildEcho(_ *testing.T, provider godi.Provider, opts conformanceOptions) conformanceRunner {
	options := []godiecho.Option{godiecho.WithLogger(quietLogger)}
	if opts.failingMiddleware {
		options = append(options,
			godiecho.WithMiddleware(func(scope godi.Scope, _ *echo.Context) error { return failingMiddleware(scope) }),
			godiecho.WithMiddlewareErrorHandler(func(*echo.Context, error) error { return echo.ErrUnauthorized }),
		)
	}
	engine := echo.New()
	engine.Use(godiecho.ScopeMiddleware(provider, options...))
	engine.GET("/probe", godiecho.Handle(func(p *conformanceProbe, c *echo.Context) error {
		return c.String(http.StatusOK, p.serve(c.Request().Context()))
	}, godiecho.WithHandlerLogger(quietLogger)))
	engine.GET("/panic", godiecho.Handle(func(p *conformanceProbe, c *echo.Context) error {
		p.serve(c.Request().Context())
		panic(panicDetail)
	}, godiecho.WithPanicRecovery(true), godiecho.WithHandlerLogger(quietLogger)))
	engine.GET("/missing", godiecho.Handle(func(*unregisteredController, *echo.Context) error { return nil },
		godiecho.WithHandlerLogger(quietLogger)))
	return recorderRunner(engine)
}

func buildFiber(t *testing.T, provider godi.Provider, opts conformanceOptions) conformanceRunner {
	options := []godifiber.Option{godifiber.WithLogger(quietLogger)}
	if opts.failingMiddleware {
		options = append(options,
			godifiber.WithMiddleware(func(scope godi.Scope, _ fiber.Ctx) error { return failingMiddleware(scope) }),
			godifiber.WithMiddlewareErrorHandler(func(fiber.Ctx, error) error { return fiber.ErrUnauthorized }),
		)
	}
	app := fiber.New()
	app.Use(godifiber.ScopeMiddleware(provider, options...))
	app.Get("/probe", godifiber.Handle(func(p *conformanceProbe, c fiber.Ctx) error {
		return c.SendString(p.serve(c.Context()))
	}, godifiber.WithHandlerLogger(quietLogger)))
	app.Get("/panic", godifiber.Handle(func(p *conformanceProbe, c fiber.Ctx) error {
		p.serve(c.Context())
		panic(panicDetail)
	}, godifiber.WithPanicRecovery(true), godifiber.WithHandlerLogger(quietLogger)))
	app.Get("/missing", godifiber.Handle(func(*unregisteredController, fiber.Ctx) error { return nil },
		godifiber.WithHandlerLogger(quietLogger)))
	return testRunner(t, func(req *http.Request) (*http.Response, error) { return app.Test(req) })
}

func conformanceTargets() []conformanceTarget {
	return []conformanceTarget{
		{name: "net/http", build: buildNetHTTP},
		{name: "chi", build: buildChi},
		{name: "gin", build: buildGin},
		{name: "echo", build: buildEcho},
		{name: "fiber", build: buildFiber},
	}
}

// conformanceProvider builds a provider with a scoped resource that records
// every instance it creates, and the probe controller.
func conformanceProvider(t *testing.T) (godi.Provider, *observations, func() []*requestResource) {
	t.Helper()
	obs := &observations{}
	var mu sync.Mutex
	var created []*requestResource

	services := godi.NewCollection()
	services.AddSingleton(func() *observations { return obs })
	services.AddScoped(func() *requestResource {
		resource := &requestResource{}
		mu.Lock()
		created = append(created, resource)
		mu.Unlock()
		return resource
	})
	services.AddScoped(func(resource *requestResource, o *observations) *conformanceProbe {
		return &conformanceProbe{resource: resource, observations: o}
	})

	provider, err := services.Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return provider, obs, func() []*requestResource {
		mu.Lock()
		defer mu.Unlock()
		return append([]*requestResource(nil), created...)
	}
}

func get(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, path, http.NoBody)
}

func assertClosedOnce(t *testing.T, resources []*requestResource) {
	t.Helper()
	if len(resources) == 0 {
		t.Fatal("no scoped resource was created")
	}
	for i, resource := range resources {
		if calls := resource.closeCalls.Load(); calls != 1 {
			t.Errorf("resource %d Close calls = %d, want 1", i, calls)
		}
	}
}

func assertGeneric500(t *testing.T, status int, body string, leaks ...string) {
	t.Helper()
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", status, http.StatusInternalServerError, body)
	}
	for _, leak := range leaks {
		if strings.Contains(body, leak) {
			t.Errorf("response leaked internal detail %q: %s", leak, body)
		}
	}
}

func TestAdapterConformance(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, target := range conformanceTargets() {
		t.Run(target.name, func(t *testing.T) {
			t.Run("scope per request is isolated", func(t *testing.T) {
				provider, obs, _ := conformanceProvider(t)
				run := target.build(t, provider, conformanceOptions{})

				for range 2 {
					if status, body := run(get("/probe")); status != http.StatusOK || body != "ok" {
						t.Fatalf("status = %d, body = %q; want 200 ok", status, body)
					}
				}

				resources, sameInstance, _ := obs.snapshot()
				if len(resources) != 2 {
					t.Fatalf("handler ran %d times, want 2", len(resources))
				}
				if resources[0] == resources[1] {
					t.Error("two requests shared one scoped instance")
				}
				for i, same := range sameInstance {
					if !same {
						t.Errorf("request %d: the request context resolved a different instance than the controller", i)
					}
				}
			})

			t.Run("scope is closed after the handler returns, not before", func(t *testing.T) {
				provider, obs, created := conformanceProvider(t)
				run := target.build(t, provider, conformanceOptions{})

				if status, body := run(get("/probe")); status != http.StatusOK {
					t.Fatalf("status = %d; body = %s", status, body)
				}

				_, _, closedDuringHandler := obs.snapshot()
				if len(closedDuringHandler) != 1 || closedDuringHandler[0] {
					t.Fatalf("scope closed during the handler: %v", closedDuringHandler)
				}
				assertClosedOnce(t, created())
			})

			t.Run("resolution failure renders a generic 500", func(t *testing.T) {
				provider, _, _ := conformanceProvider(t)
				run := target.build(t, provider, conformanceOptions{})

				status, body := run(get("/missing"))
				assertGeneric500(t, status, body, "unregisteredController", "integrationtests", "not found")
			})

			t.Run("middleware error handler sets the status", func(t *testing.T) {
				provider, obs, created := conformanceProvider(t)
				run := target.build(t, provider, conformanceOptions{failingMiddleware: true})

				status, body := run(get("/probe"))
				if status != http.StatusUnauthorized {
					t.Fatalf("status = %d, want %d; body = %s", status, http.StatusUnauthorized, body)
				}
				if strings.Contains(body, middlewareDetail) {
					t.Errorf("response leaked middleware error: %s", body)
				}
				if resources, _, _ := obs.snapshot(); len(resources) != 0 {
					t.Error("the handler ran after the middleware failed")
				}
				assertClosedOnce(t, created())
			})

			t.Run("panic recovery renders a generic 500 and closes the scope", func(t *testing.T) {
				provider, _, created := conformanceProvider(t)
				run := target.build(t, provider, conformanceOptions{})

				status, body := run(get("/panic"))
				assertGeneric500(t, status, body, panicDetail, "hunter2")
				assertClosedOnce(t, created())
			})
		})
	}
}
