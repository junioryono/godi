module github.com/junioryono/godi/huma/v6

go 1.26.0

require (
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/junioryono/godi/v6 v6.0.0
	github.com/stretchr/testify v1.12.1
	go.uber.org/goleak v1.3.0
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

replace github.com/junioryono/godi/v6 => ../
