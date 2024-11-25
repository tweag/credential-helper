package agent

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tweag/credential-helper/cache"
)

func TestInvalidJSON(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	cachingAgent, lis := setup()
	wg := sync.WaitGroup{}
	wg.Add(1)
	var serveErr error
	go func() {
		defer wg.Done()
		serveErr = cachingAgent.Serve(ctx)
	}()

	// sending invalid JSON
	clientConn := lis.dial()
	_, err := clientConn.Write([]byte("foo"))
	assert.NoError(err)

	// expecting a response indicating a failure to decode the request
	responseBuf := make([]byte, 512)
	n, err := clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"error\",\"payload\":\"invalid json in request\"}\n"), responseBuf[:n])

	// sending any message should now fail
	// because the server closes the connection
	// after failing to decode a request
	_, err = clientConn.Write([]byte("{}"))
	assert.Error(err)

	assert.NoError(clientConn.Close())
	// initiate shutdown
	cachingAgent.handleShutdown()

	wg.Wait()
	assert.NoError(serveErr)
}

func TestUnknownMessage(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	cachingAgent, lis := setup()
	wg := sync.WaitGroup{}
	wg.Add(1)
	var serveErr error
	go func() {
		defer wg.Done()
		serveErr = cachingAgent.Serve(ctx)
	}()

	// sending valid JSON with invalid schema
	clientConn := lis.dial()
	_, err := clientConn.Write([]byte("{}"))
	assert.NoError(err)

	// expecting a response indicating a an unknown method
	responseBuf := make([]byte, 512)
	n, err := clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"error\",\"payload\":\"unknown method\"}\n"), responseBuf[:n])

	// sending more messages still works
	// because the server keeps the connection open
	_, err = clientConn.Write([]byte("{}"))
	assert.NoError(err)
	n, err = clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"error\",\"payload\":\"unknown method\"}\n"), responseBuf[:n])

	assert.NoError(clientConn.Close())
	// initiate shutdown
	cachingAgent.handleShutdown()

	wg.Wait()
	assert.NoError(serveErr)
}

func TestReadWrite(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	cachingAgent, lis := setup()
	wg := sync.WaitGroup{}
	wg.Add(1)
	var serveErr error
	go func() {
		defer wg.Done()
		serveErr = cachingAgent.Serve(ctx)
	}()

	// sending a retrieve for a key that does not exist yet
	clientConn := lis.dial()
	_, err := clientConn.Write([]byte("{\"method\":\"retrieve\", \"payload\":\"foo\"}"))
	assert.NoError(err)

	// expecting a response indicating a cache miss
	responseBuf := make([]byte, 512)
	n, err := clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"cache-miss\"}\n"), responseBuf[:n])

	// sending a store for a key
	_, err = clientConn.Write([]byte("{\"method\":\"store\", \"payload\":{\"cacheKey\":\"foo\",\"response\":{\"expires\":\"2006-01-02T15:04:05Z07:00\",\"headers\":{\"x-test\":[\"bar\"]}}}}"))
	assert.NoError(err)
	n, err = clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"ok\"}\n"), responseBuf[:n])

	// sending a retrieve for a key that exists
	_, err = clientConn.Write([]byte("{\"method\":\"retrieve\", \"payload\":\"foo\"}"))
	assert.NoError(err)
	n, err = clientConn.Read(responseBuf)
	assert.NoError(err)
	// expecting a cached response
	assert.Equal([]byte("{\"status\":\"ok\",\"payload\":{\"expires\":\"2006-01-02T15:04:05Z07:00\",\"headers\":{\"x-test\":[\"bar\"]}}}\n"), responseBuf[:n])

	// updating a key
	_, err = clientConn.Write([]byte("{\"method\":\"store\", \"payload\":{\"cacheKey\":\"foo\",\"response\":{\"expires\":\"2006-01-02T15:04:05Z07:00\",\"headers\":{\"x-test\":[\"baz\"]}}}}"))
	assert.NoError(err)
	n, err = clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"ok\"}\n"), responseBuf[:n])

	// retrieving the updated key
	_, err = clientConn.Write([]byte("{\"method\":\"retrieve\", \"payload\":\"foo\"}"))
	assert.NoError(err)
	n, err = clientConn.Read(responseBuf)
	assert.NoError(err)
	// expecting a cached response
	assert.Equal([]byte("{\"status\":\"ok\",\"payload\":{\"expires\":\"2006-01-02T15:04:05Z07:00\",\"headers\":{\"x-test\":[\"baz\"]}}}\n"), responseBuf[:n])

	// pruning the cache
	_, err = clientConn.Write([]byte("{\"method\":\"prune\"}"))
	assert.NoError(err)
	n, err = clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"ok\"}\n"), responseBuf[:n])

	// sending a retrieve for a key that existed before pruning
	_, err = clientConn.Write([]byte("{\"method\":\"retrieve\", \"payload\":\"foo\"}"))
	assert.NoError(err)
	// expecting a response indicating a cache miss
	n, err = clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"cache-miss\"}\n"), responseBuf[:n])

	assert.NoError(clientConn.Close())
	// initiate shutdown
	cachingAgent.handleShutdown()

	wg.Wait()
	assert.NoError(serveErr)
}

func TestShutdown(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	cachingAgent, lis := setup()
	wg := sync.WaitGroup{}
	wg.Add(1)
	var serveErr error
	go func() {
		defer wg.Done()
		serveErr = cachingAgent.Serve(ctx)
	}()

	// initiate shutdown
	clientConn := lis.dial()
	_, err := clientConn.Write([]byte("{\"method\":\"shutdown\"}"))
	assert.NoError(err)
	responseBuf := make([]byte, 512)
	n, err := clientConn.Read(responseBuf)
	assert.NoError(err)
	assert.Equal([]byte("{\"status\":\"ok\"}\n"), responseBuf[:n])

	assert.NoError(clientConn.Close())

	wg.Wait()
	assert.NoError(serveErr)
}

func setup() (CachingAgent, *testListener) {
	lis := newTestListener()

	return CachingAgent{
		cache:        cache.NewMemCache(),
		lis:          lis,
		shutdownChan: make(chan struct{}),
	}, lis
}

type testListener struct {
	connectionQueue chan net.Conn
}

func newTestListener() *testListener {
	return &testListener{
		connectionQueue: make(chan net.Conn),
	}
}

func (l *testListener) dial() net.Conn {
	clientConn, serverConn := net.Pipe()
	l.connectionQueue <- serverConn
	return clientConn
}

func (l *testListener) Accept() (net.Conn, error) {
	conn, ok := <-l.connectionQueue
	if !ok {
		return nil, errors.New("testListener closed")
	}
	return conn, nil
}

func (l *testListener) Close() error {
	close(l.connectionQueue)
	return nil
}

func (l *testListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "testListener", Net: "testNet"}
}
