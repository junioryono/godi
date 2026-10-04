// Package integrationtests holds godi's tests that need several integrations
// at once, such as Huma mounted on each router adapter.
//
// It is a separate, never-released module (see scripts/modules.txt) so that
// no integration's go.mod depends on the other integrations' routers. Local
// replace directives point it at the working tree, and `make test` runs it
// with the other modules.
package integrationtests
