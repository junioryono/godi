package huma_test

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"

	godihuma "github.com/junioryono/godi/huma/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newCapturingLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

func TestContract_DefaultResolutionErrorUsesLogger(t *testing.T) {
	logger, logs := newCapturingLogger()

	_, err := godihuma.Handle((*userController).Greet, godihuma.WithLogger(logger))(context.Background(), &greetInput{})

	require.Error(t, err)
	assert.Contains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "no scope")
	assert.NotContains(t, err.Error(), "no scope")
}

func TestContract_SanitizedControllerErrorUsesLogger(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()
	logger, logs := newCapturingLogger()

	_, err := godihuma.Handle((*userController).Fail, godihuma.WithLogger(logger))(ctx, &greetInput{})

	require.Error(t, err)
	assert.Contains(t, logs.String(), errBoom.Error())
	assert.NotContains(t, err.Error(), errBoom.Error())
}

func TestContract_DefaultLoggerIsSlogDefault(t *testing.T) {
	logger, logs := newCapturingLogger()
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, err := godihuma.Handle((*userController).Greet, godihuma.WithLogger(nil))(context.Background(), &greetInput{})

	require.Error(t, err)
	assert.Contains(t, logs.String(), "no scope")
}
