godi Documentation
==================

**godi** - Simple, powerful dependency injection for Go. No magic, just functions.

Quick Example
-------------

.. code-block:: go

   // Define your service
   type Greeter struct {
       name string
   }
   
   func NewGreeter() *Greeter {
       return &Greeter{name: "World"}
   }
   
   // Set up DI
   collection := godi.NewCollection()
   collection.AddSingleton(NewGreeter)
   
   provider, _ := collection.Build()
   defer provider.Close()
   
   // Use it
   greeter, _ := godi.Resolve[*Greeter](provider)
   fmt.Println(greeter.Greet()) // Hello, World!

That's it! Start simple, add features as you need them.

Why godi?
---------

- ✅ **Simple** - Just functions, no annotations or code generation
- ✅ **Type-safe** - Full compile-time checking with generics
- ✅ **Testable** - Swap implementations instantly with modules
- ✅ **Scalable** - From simple CLIs to complex web applications
- ✅ **Fast** - Minimal overhead, production-ready

.. toctree::
   :maxdepth: 2
   :caption: Learn
   :hidden:

   getting-started.md
   core-concepts.md

.. toctree::
   :maxdepth: 2
   :caption: Guides
   :hidden:

   guides/web-apps-http.md
   guides/web-apps-gin.md
   guides/web-apps-mux.md
   guides/testing.md
   guides/modules.md
   guides/advanced.md

.. toctree::
   :maxdepth: 2
   :caption: Reference
   :hidden:

   reference/api.md
   reference/errors.md
   reference/faq.md
   reference/changelog.md