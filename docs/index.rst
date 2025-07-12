godi Documentation
==================

**godi** brings type-safe dependency injection to Go with zero magic. 
Start simple, scale seamlessly.

Quick Start
-----------

.. code-block:: go

   // 1. Register your services
   services := godi.NewServiceCollection()
   services.AddSingleton(NewLogger)
   services.AddScoped(NewUserService)

   // 2. Build the container
   provider, _ := services.BuildServiceProvider()
   defer provider.Close()

   // 3. Use your services
   userService, _ := godi.Resolve[*UserService](provider)

That's it! No annotations, no reflection magic, just functions.

Why godi?
---------

✅ **Never update constructors everywhere** - Change once, godi handles the rest  
✅ **Perfect for web apps** - Request isolation with scopes  
✅ **Testing made easy** - Swap implementations instantly  
✅ **Start simple** - Use only what you need  
✅ **Type-safe** - Compile-time checking with generics  

.. toctree::
   :maxdepth: 2
   :caption: Get Started
   :hidden:

   overview/install.md
   tutorials/quick-start.md
   tutorials/getting-started.md
   overview/why-di.md

.. toctree::
   :maxdepth: 2
   :caption: Learn
   :hidden:

   overview/concepts.md
   tutorials/simple-vs-modules.md
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
   overview/comparison.md