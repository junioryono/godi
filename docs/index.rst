godi Documentation
==================

  "Dependency injection is not about the tools, it's about the design.
  But having great tools makes great design achievable."

**godi** brings modern, type-safe dependency injection to Go, making your applications 
more **maintainable**, **testable**, and **scalable**. Built on Uber's dig with a 
Microsoft-inspired API, it provides the power you need with the simplicity you want.

Why godi?
---------

1. **Never touch constructors again** when adding dependencies
2. **Automatic lifecycle management** for resources
3. **Request scoping** for web applications
4. **Zero boilerplate** dependency wiring
5. **Type-safe** with compile-time verification
6. **Testable** by design

Quick Example
-------------

.. code-block:: go

   // Define your services
   services := godi.NewServiceCollection()
   services.AddSingleton(NewLogger)
   services.AddScoped(NewDatabase)
   services.AddTransient(NewEmailService)

   // Build the container
   provider, _ := services.BuildServiceProvider()
   defer provider.Close()

   // Resolve and use
   logger, _ := godi.Resolve[Logger](provider)
   logger.Log("Application started!")

.. toctree::
   :maxdepth: 2
   :caption: Overview
   :hidden:

   overview/install.md
   overview/why-di.md
   overview/concepts.md
   overview/comparison.md

.. toctree::
   :maxdepth: 2
   :caption: Tutorials
   :hidden:

   tutorials/getting-started.md
   tutorials/web-application.md
   tutorials/testing.md
   tutorials/microservices.md

.. toctree::
   :maxdepth: 2
   :caption: How-to Guides
   :hidden:

   howto/register-services.md
   howto/use-scopes.md
   howto/keyed-services.md
   howto/service-groups.md
   howto/modules.md
   howto/decorators.md
   howto/parameter-objects.md
   howto/disposal.md
   howto/advanced-patterns.md

.. toctree::
   :maxdepth: 2
   :caption: Reference
   :hidden:

   reference/api.md
   reference/lifetimes.md
   reference/errors.md
   reference/options.md
   reference/changelog.md

.. toctree::
   :maxdepth: 2
   :caption: Conceptual Guides
   :hidden:

   guides/architecture.md
   guides/best-practices.md
   guides/migration.md
   guides/performance.md
   guides/troubleshooting.md