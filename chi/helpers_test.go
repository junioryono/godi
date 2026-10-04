package chi

import "net/http"

// The configuration defaults live in godihttp. These helpers observe them
// through the public API: options receive the config that ScopeMiddleware
// and Handle normalize after all options have run.

func defaultConfig() *Config {
	var cfg *Config
	ScopeMiddleware(nil, func(c *Config) { cfg = c })
	return cfg
}

func defaultHandlerConfig() *HandlerConfig {
	var cfg *HandlerConfig
	Handle(func(struct{}, http.ResponseWriter, *http.Request) {}, func(c *HandlerConfig) { cfg = c })
	return cfg
}
