.. toctree::
   :maxdepth: 2
   :caption: Getting Started
   :hidden:

   installation
   core-concepts

.. toctree::
   :maxdepth: 2
   :caption: Fundamentals
   :hidden:

   service-lifetimes
   service-registration
   dependency-resolution
   scopes-isolation
   resource-management
   modules

.. toctree::
   :maxdepth: 2
   :caption: Advanced Features
   :hidden:

   keyed-services
   service-groups
   parameter-objects
   result-objects
   interface-registration

.. toctree::
   :maxdepth: 2
   :caption: Quick Links
   :hidden:

   GitHub <https://github.com/junioryono/godi>
   Go Packages <https://pkg.go.dev/github.com/junioryono/godi/v4>
   Changelog <https://github.com/junioryono/godi/releases>

.. toctree::
   :maxdepth: 2
   :caption: Community
   :hidden:

   Contributing <https://github.com/junioryono/godi/blob/main/CONTRIBUTING.md>
   Discussions <https://github.com/junioryono/godi/discussions>
   Issues <https://github.com/junioryono/godi/issues>

godi - Dependency Injection with Service Lifetimes for Go
==========================================================

.. image:: https://pkg.go.dev/badge/github.com/junioryono/godi/v4
   :target: https://pkg.go.dev/github.com/junioryono/godi/v4
   :alt: GoDoc

.. image:: https://img.shields.io/github/release/junioryono/godi.svg
   :target: https://github.com/junioryono/godi/releases
   :alt: Github release

.. image:: https://github.com/junioryono/godi/actions/workflows/test.yml/badge.svg
   :target: https://github.com/junioryono/godi/actions/workflows/test.yml
   :alt: Build Status

.. image:: https://codecov.io/gh/junioryono/godi/branch/main/graph/badge.svg
   :target: https://codecov.io/gh/junioryono/godi
   :alt: Coverage Status

.. image:: https://goreportcard.com/badge/github.com/junioryono/godi
   :target: https://goreportcard.com/report/github.com/junioryono/godi
   :alt: Go Report Card

A sophisticated dependency injection container for Go with service lifetimes, type safety, and automatic dependency resolution.

Quick Example
-------------

.. code-block:: go

   // Define your services
   func NewLogger() Logger { return &logger{} }
   func NewDatabase(logger Logger) Database { return &database{logger} }
   func NewUserService(db Database) UserService { return &userService{db} }

   // Wire everything together
   services := godi.NewCollection()
   services.AddSingleton(NewLogger)
   services.AddSingleton(NewDatabase)
   services.AddScoped(NewUserService)

   // Build and use
   provider, _ := services.Build()
   defer provider.Close()

   userService := godi.MustResolve[UserService](provider)

Key Features
------------

**Service Lifetimes**
   - **Singleton**: One instance for the entire application
   - **Scoped**: One instance per scope (perfect for HTTP requests)
   - **Transient**: New instance every time

**Type Safety**
   - Generic resolution with compile-time type checking
   - No runtime type assertions needed
   - Full IDE autocomplete support

**Automatic Resolution**
   - Analyzes constructors and builds dependency graph
   - Detects circular dependencies at build time
   - Validates lifetime rules before runtime

**Advanced Features**
   - **Keyed Services**: Multiple implementations of the same interface
   - **Service Groups**: Batch operations on related services
   - **Parameter Objects**: Clean constructors with ``godi.In``
   - **Result Objects**: Register multiple services with ``godi.Out``
   - **Modules**: Organize services into reusable packages

Getting Started
---------------

**Installation**

.. code-block:: bash

   go get github.com/junioryono/godi/v4

**Requirements**: Go 1.21 or later

Start with our :doc:`installation` guide to set up godi in your project.

Why godi?
---------

- **Zero Code Generation**: Pure runtime dependency injection
- **Thread-Safe**: Fully concurrent-safe operations
- **Production Ready**: Battle-tested in real applications
- **Clean API**: Intuitive and idiomatic Go
- **Excellent Errors**: Detailed error messages for debugging

Next Steps
----------

- :doc:`installation` - Set up godi in your project
- :doc:`core-concepts` - Understand the fundamentals

License
-------

MIT License - see `LICENSE <https://github.com/junioryono/godi/blob/main/LICENSE>`_
