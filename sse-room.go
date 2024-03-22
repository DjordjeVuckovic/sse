package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

type Client struct {
	channel chan string
	roomId  string
}

type SSEMessage struct {
	roomId  string
	message string
}

type SSEBroker struct {
	clients           map[Client]struct{}
	addClient         chan Client
	removeClient      chan Client
	messages          chan SSEMessage
	ctx               context.Context
	cancelFunc        context.CancelFunc
	clientsMutex      sync.RWMutex
	additionalHeaders map[string]string
}

type SSEOptions func(*SSEBroker)

func WithAdditionalHeaders(headers map[string]string) SSEOptions {
	return func(b *SSEBroker) {
		b.additionalHeaders = headers
	}
}

func NewSSEBroker(ctx context.Context, opts ...SSEOptions) *SSEBroker {
	newCtx, cancelFunc := context.WithCancel(ctx)
	broker := &SSEBroker{
		clients:           make(map[Client]struct{}),
		addClient:         make(chan Client),
		removeClient:      make(chan Client),
		messages:          make(chan SSEMessage),
		ctx:               newCtx,
		cancelFunc:        cancelFunc,
		additionalHeaders: make(map[string]string),
		clientsMutex:      sync.RWMutex{},
	}
	for _, opt := range opts {
		opt(broker)
	}
	go broker.listen()
	return broker
}

func (b *SSEBroker) listen() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case client := <-b.addClient:
			b.clientsMutex.Lock()
			b.clients[client] = struct{}{}
			b.clientsMutex.Unlock()
		case client := <-b.removeClient:
			b.clientsMutex.Lock()
			delete(b.clients, client)
			close(client.channel)
			b.clientsMutex.Unlock()
		case taskMsg := <-b.messages:
			for client := range b.clients {
				if client.roomId == taskMsg.roomId {
					select {
					case client.channel <- taskMsg.message:
					default:
						slog.Info("Dropping message to a client.")
					}
				}
			}
		}
	}
}

func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	sessionId := r.URL.Query().Get("sessionId")
	slog.Info("Auth token", "authToken", sessionId)
	// Extract roomId from query params or other means
	roomId := r.URL.Query().Get("roomId")
	if roomId == "" {
		http.Error(w, "Task ID required", http.StatusBadRequest)
		return
	}

	// Setup headers and flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Setup client for this connection
	clientChan := make(chan string)
	client := Client{channel: clientChan, roomId: roomId}
	b.addClient <- client
	defer func() {
		slog.Info("Closing client connection")
		slog.Info("Client: ", "client", client)
		slog.Info("Auth token", "authToken", sessionId)
		b.removeClient <- client
	}()

	// Listen for messages or connection close
	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg := <-clientChan:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func (b *SSEBroker) Broadcast(taskId, msg string) {
	b.messages <- SSEMessage{roomId: taskId, message: msg}
}

func (b *SSEBroker) Shutdown() {
	b.cancelFunc()
}
