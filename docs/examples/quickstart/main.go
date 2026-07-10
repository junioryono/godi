// Package main demonstrates a complete godi container lifecycle.
package main

import (
	"fmt"
	"log"

	"github.com/junioryono/godi/v5"
)

type logger struct{}

func (logger) Print(message string) {
	fmt.Println(message)
}

type greeter struct {
	logger *logger
}

func newLogger() *logger {
	return &logger{}
}

func newGreeter(logger *logger) *greeter {
	return &greeter{logger: logger}
}

func main() {
	services := godi.NewCollection()
	services.AddSingleton(newLogger)
	services.AddSingleton(newGreeter)

	provider, err := services.Build()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := provider.Close(); err != nil {
			log.Printf("close provider: %v", err)
		}
	}()

	godi.MustResolve[*greeter](provider).logger.Print("Hello from godi")
}
