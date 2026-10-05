package godi_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/junioryono/godi/v6"
)

// Types shared by the examples.

type Config struct{ DSN string }

type Logger struct{ prefix string }

func (l *Logger) Log(msg string) { fmt.Println(l.prefix + msg) }

type Database struct{ name string }

func (db *Database) Close() error {
	fmt.Println("close", db.name)
	return nil
}

type UserService struct {
	db  *Database
	log *Logger
}

func (s *UserService) Greet(name string) { s.log.Log("hello, " + name + " (db: " + s.db.name + ")") }

func NewConfig() *Config { return &Config{DSN: "postgres://app"} }

func NewLogger() *Logger { return &Logger{prefix: "[app] "} }

func NewDatabase(cfg *Config) *Database { return &Database{name: cfg.DSN} }

func NewUserService(db *Database, log *Logger) *UserService {
	return &UserService{db: db, log: log}
}

// The basic workflow: register constructors, Build (which validates the
// graph and creates singletons), resolve, and Close.
func ExampleNewCollection() {
	services := godi.NewCollection()
	services.AddSingleton(NewConfig)
	services.AddSingleton(NewLogger)
	services.AddSingleton(NewDatabase)
	services.AddScoped(NewUserService)

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer provider.Close()

	scope, err := provider.CreateScope(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	defer scope.Close()

	users, err := godi.Resolve[*UserService](scope)
	if err != nil {
		fmt.Println(err)
		return
	}
	users.Greet("gopher")
	// Output:
	// [app] hello, gopher (db: postgres://app)
	// close postgres://app
}

// RequestState is a scoped service: one per scope, closed with its scope.
type RequestState struct{ id int }

func (r *RequestState) Close() error {
	fmt.Println("close request", r.id)
	return nil
}

// A scoped service is shared within a scope, distinct across scopes, and
// disposed when its scope is closed.
func ExampleProvider_CreateScope() {
	next := 0
	services := godi.NewCollection()
	services.AddScoped(func() *RequestState {
		next++
		return &RequestState{id: next}
	})

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer provider.Close()

	first, _ := provider.CreateScope(context.Background())
	a := godi.MustResolve[*RequestState](first)
	b := godi.MustResolve[*RequestState](first)
	fmt.Println("same within a scope:", a == b)

	second, _ := provider.CreateScope(context.Background())
	c := godi.MustResolve[*RequestState](second)
	fmt.Println("distinct across scopes:", a != c)

	_ = first.Close()
	_ = second.Close()
	// Output:
	// same within a scope: true
	// distinct across scopes: true
	// close request 1
	// close request 2
}

// Cache is registered several times under different names.
type Cache struct{ region string }

// Name registers a keyed service; resolve it with ResolveKeyed or a
// name:"..." struct tag.
func ExampleName() {
	services := godi.NewCollection()
	services.AddSingleton(func() *Cache { return &Cache{region: "us"} }, godi.Name("primary"))
	services.AddSingleton(func() *Cache { return &Cache{region: "eu"} }, godi.Name("replica"))

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer provider.Close()

	primary := godi.MustResolveKeyed[*Cache](provider, "primary")
	replica := godi.MustResolveKeyed[*Cache](provider, "replica")
	fmt.Println(primary.region, replica.region)
	// Output: us eu
}

// Handler is the member type of the "routes" group.
type Handler interface{ Route() string }

type route string

func (r route) Route() string { return string(r) }

// Group collects several registrations of one type; ResolveGroup (or a
// group:"..." slice field) returns them in registration order.
func ExampleGroup() {
	services := godi.NewCollection()
	services.AddSingleton(func() Handler { return route("/users") }, godi.Group("routes"))
	services.AddSingleton(func() Handler { return route("/orders") }, godi.Group("routes"))

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer provider.Close()

	handlers := godi.MustResolveGroup[Handler](provider, "routes")
	for _, h := range handlers {
		fmt.Println(h.Route())
	}
	// Output:
	// /users
	// /orders
}

// ServerParams is a parameter object: godi fills its exported fields.
type ServerParams struct {
	godi.In

	Config   *Config
	Primary  *Cache    `name:"primary"`
	Logger   *Logger   `optional:"true"`
	Handlers []Handler `group:"routes"`
}

type Server struct{ summary string }

// In lets a constructor take its dependencies as struct fields, with
// name, group, and optional tags.
func ExampleIn() {
	services := godi.NewCollection()
	services.AddSingleton(NewConfig)
	services.AddSingleton(func() *Cache { return &Cache{region: "us"} }, godi.Name("primary"))
	services.AddSingleton(func() Handler { return route("/users") }, godi.Group("routes"))
	services.AddSingleton(func(p ServerParams) *Server {
		return &Server{summary: fmt.Sprintf("dsn=%s cache=%s logger=%v routes=%d",
			p.Config.DSN, p.Primary.region, p.Logger != nil, len(p.Handlers))}
	})

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer provider.Close()

	fmt.Println(godi.MustResolve[*Server](provider).summary)
	// Output: dsn=postgres://app cache=us logger=false routes=1
}

// Storage is a result object: each field is registered as its own service.
type Storage struct {
	godi.Out

	Primary *Cache  `name:"primary"`
	Replica *Cache  `name:"replica"`
	Health  Handler `group:"routes"`
}

// Out lets one constructor provide several services.
func ExampleOut() {
	services := godi.NewCollection()
	services.AddSingleton(func() Storage {
		return Storage{
			Primary: &Cache{region: "us"},
			Replica: &Cache{region: "eu"},
			Health:  route("/health"),
		}
	})

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer provider.Close()

	fmt.Println(godi.MustResolveKeyed[*Cache](provider, "replica").region)
	fmt.Println(godi.MustResolveGroup[Handler](provider, "routes")[0].Route())
	// Output:
	// eu
	// /health
}

// Store is decorated with logging.
type Store interface{ Get(key string) string }

type memoryStore struct{}

func (memoryStore) Get(key string) string { return "value of " + key }

type loggingStore struct {
	next Store
	log  *Logger
}

func (s loggingStore) Get(key string) string {
	s.log.Log("get " + key)
	return s.next.Get(key)
}

// Decorate wraps a registered service; the decorator's other parameters are
// injected like a constructor's.
func ExampleDecorate() {
	services := godi.NewCollection()
	services.AddSingleton(NewLogger)
	services.AddSingleton(func() Store { return memoryStore{} })
	services.AddModules(godi.Decorate(func(next Store, log *Logger) Store {
		return loggingStore{next: next, log: log}
	}))

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer provider.Close()

	fmt.Println(godi.MustResolve[Store](provider).Get("answer"))
	// Output:
	// [app] get answer
	// value of answer
}

// Mailer is replaced by a fake in tests.
type Mailer interface{ Send(to string) error }

type smtpMailer struct{}

func (smtpMailer) Send(to string) error { return errors.New("no SMTP server in tests") }

type fakeMailer struct{ sent []string }

func (m *fakeMailer) Send(to string) error {
	m.sent = append(m.sent, to)
	return nil
}

// MailModule is the production wiring.
var MailModule = godi.NewModule("mail",
	godi.AddSingleton(func() Mailer { return smtpMailer{} }),
)

// ReplaceSingleton swaps a registration, typically to install a fake in a
// test that reuses the application's modules.
func ExampleReplaceSingleton() {
	fake := &fakeMailer{}

	services := godi.NewCollection()
	services.AddModules(
		MailModule,
		godi.ReplaceSingleton(func() Mailer { return fake }),
	)

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer provider.Close()

	mailer := godi.MustResolve[Mailer](provider)
	fmt.Println(mailer.Send("gopher@example.com"), fake.sent)
	// Output: <nil> [gopher@example.com]
}

// Validate checks the wiring without running any constructor, so tests
// need no database or network.
func ExampleValidate() {
	services := godi.NewCollection()
	services.AddSingleton(func(db *Database) *UserService {
		fmt.Println("never called")
		return &UserService{db: db}
	})

	err := godi.Validate(services)
	fmt.Println(errors.Is(err, godi.ErrServiceNotFound))

	var missing *godi.MissingDependencyError
	if errors.As(err, &missing) {
		fmt.Println(missing.ServiceType, "requires", missing.DependencyType)
	}
	// Output:
	// true
	// *godi_test.UserService requires *godi_test.Database
}

// HTTPServer has a graceful shutdown, like *http.Server.
type HTTPServer struct{ db *Database }

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	_, hasDeadline := ctx.Deadline()
	fmt.Println("shutdown server, deadline:", hasDeadline)
	return nil
}

// Shutdown disposes a provider like Close but stops waiting at the context's
// deadline. Resources are disposed in reverse creation order (consumers
// before their dependencies), and a Shutdowner receives the context.
func ExampleShutdown() {
	services := godi.NewCollection()
	services.AddSingleton(NewConfig)
	services.AddSingleton(NewDatabase)
	services.AddSingleton(func(db *Database) *HTTPServer { return &HTTPServer{db: db} })

	provider, err := services.Build()
	if err != nil {
		fmt.Println(err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := godi.Shutdown(ctx, provider); err != nil {
		fmt.Println(err)
	}
	// Output:
	// shutdown server, deadline: true
	// close postgres://app
}

// Session is scoped; a singleton must not capture it.
type Session struct{}

type SessionCache struct{ session *Session }

// Explain returns an error's one-line message followed by its remediation
// hints; %+v prints the same.
func ExampleExplain() {
	services := godi.NewCollection()
	services.AddScoped(func() *Session { return &Session{} })
	services.AddSingleton(func(s *Session) *SessionCache { return &SessionCache{session: s} })

	_, err := services.Build()
	fmt.Println(err)
	fmt.Println()
	explained := godi.Explain(err)
	fmt.Println(strings.TrimPrefix(explained, err.Error()+"\n\n"))
	// Output:
	// build failed during validation phase: lifetime validation failed: lifetime conflict: *godi_test.SessionCache (Singleton) cannot depend on *godi_test.Session (Scoped)
	//
	// Singleton services are created once and live for the application lifetime.
	// Scoped services are created per-scope and may have different values in different scopes.
	// A singleton depending on a scoped service would capture a single scope's value,
	// which is almost certainly not what you want.
	//
	// To resolve this:
	//   • Change *godi_test.SessionCache to Scoped lifetime
	//   • Change *godi_test.Session to Singleton lifetime
	//   • Pass *godi_test.Session to *godi_test.SessionCache's methods per call instead of holding it
}
