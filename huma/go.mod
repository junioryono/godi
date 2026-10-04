module github.com/junioryono/godi/huma/v5

go 1.26.0

require (
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/junioryono/godi/v5 v5.1.0
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

replace github.com/junioryono/godi/v5 => ../
