module github.com/junioryono/godi/echov5/v5

go 1.26.0

require (
	github.com/junioryono/godi/v5 v5.1.0
	github.com/labstack/echo/v5 v5.4.0
	github.com/stretchr/testify v1.12.1
)

require (
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/junioryono/godi/v5 => ../
