module github.com/junioryono/godi/chi/v5

go 1.26.0

require (
	github.com/junioryono/godi/http/v5 v5.1.0
	github.com/junioryono/godi/v5 v5.1.0
	github.com/stretchr/testify v1.12.1
	go.uber.org/goleak v1.3.0
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

replace github.com/junioryono/godi/v5 => ../

replace github.com/junioryono/godi/http/v5 => ../http
