.. toctree::
   :maxdepth: 2
   :caption: Getting Started
   :hidden:

   getting-started/index
   getting-started/01-installation
   getting-started/02-first-container
   getting-started/03-adding-services
   getting-started/04-using-lifetimes
   getting-started/05-http-integration
   getting-started/06-next-steps

.. toctree::
   :maxdepth: 2
   :caption: Concepts
   :hidden:

   concepts/how-it-works
   concepts/lifetimes
   concepts/scopes
   concepts/modules

.. toctree::
   :maxdepth: 2
   :caption: Guides
   :hidden:

   guides/web-applications
   guides/testing
   guides/error-handling
   guides/migration

.. toctree::
   :maxdepth: 2
   :caption: Features
   :hidden:

   features/keyed-services
   features/service-groups
   features/parameter-objects
   features/result-objects
   features/interface-binding
   features/resource-cleanup

.. toctree::
   :maxdepth: 2
   :caption: Integrations
   :hidden:

   integrations/gin
   integrations/chi
   integrations/echo
   integrations/fiber
   integrations/net-http

.. toctree::
   :maxdepth: 2
   :caption: Reference
   :hidden:

   GitHub <https://github.com/junioryono/godi>
   API Docs <https://pkg.go.dev/github.com/junioryono/godi/v4>
   Changelog <https://github.com/junioryono/godi/releases>

godi
====

**Dependency injection that gets out of your way.**

godi automatically wires up your Go applications. Define your services, specify their lifetimes, and let godi handle the rest.

.. code-block:: go

   services := godi.NewCollection()
   services.AddSingleton(NewLogger)
   services.AddSingleton(NewDatabase)
   services.AddScoped(NewUserService)

   provider, _ := services.Build()
   defer provider.Close()

   userService := godi.MustResolve[UserService](provider)

Why godi?
---------

.. list-table::
   :widths: 25 75
   :header-rows: 1

   * - Feature
     - Benefit
   * - **Automatic wiring**
     - No manual constructor calls
   * - **Three lifetimes**
     - Singleton, Scoped, Transient
   * - **Compile-time safety**
     - Generic type resolution
   * - **Zero codegen**
     - Pure runtime, no build steps

Get Started in 5 Minutes
------------------------

Install godi:

.. code-block:: bash

   go get github.com/junioryono/godi/v4

Create your first container:

.. code-block:: go

   package main

   import (
       "fmt"
       "github.com/junioryono/godi/v4"
   )

   type Logger struct{}
   func (l *Logger) Log(msg string) { fmt.Println(msg) }

   type UserService struct {
       logger *Logger
   }

   func NewUserService(logger *Logger) *UserService {
       return &UserService{logger: logger}
   }

   func main() {
       services := godi.NewCollection()
       services.AddSingleton(func() *Logger { return &Logger{} })
       services.AddSingleton(NewUserService)

       provider, _ := services.Build()
       defer provider.Close()

       users := godi.MustResolve[*UserService](provider)
       users.logger.Log("Hello, godi!")
   }

**Ready for more?** Start the :doc:`getting-started/index`.

Quick Links
-----------

**Learning godi**

- :doc:`getting-started/index` - Build your first app in 5 minutes
- :doc:`concepts/lifetimes` - Singleton, Scoped, and Transient explained
- :doc:`guides/web-applications` - Complete web app patterns

**Framework Integrations**

- :doc:`integrations/gin` - Gin web framework
- :doc:`integrations/chi` - Chi router
- :doc:`integrations/echo` - Echo framework
- :doc:`integrations/fiber` - Fiber framework
- :doc:`integrations/net-http` - Standard library

**Advanced Features**

- :doc:`features/keyed-services` - Multiple implementations
- :doc:`features/parameter-objects` - Simplify constructors
- :doc:`concepts/modules` - Organize large apps

License
-------

MIT License - see `LICENSE <https://github.com/junioryono/godi/blob/main/LICENSE>`_
