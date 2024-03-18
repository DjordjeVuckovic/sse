package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func enableCORS(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set(
			"Access-Control-Allow-Methods",
			"POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set(
			"Access-Control-Allow-Headers",
			"Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Api-Key")

		if r.Method == "OPTIONS" {
			return
		}

		next.ServeHTTP(w, r)
	}
}

func main() {
	server := http.NewServeMux()
	ctx := context.Background()
	broker := NewSSEBroker(ctx)
	defer func() {
		broker.Shutdown()
	}()
	server.Handle("/", enableCORS(server))
	server.Handle("/events", enableCORS(broker))
	server.HandleFunc("GET /live", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Live!"))
	})
	server.HandleFunc("GET /broadcast", func(w http.ResponseWriter, r *http.Request) {
		taskId := r.URL.Query().Get("taskId")
		if taskId == "" {
			http.Error(w, "Task ID is required", http.StatusBadRequest)
			return
		}

		message := r.URL.Query().Get("message")
		if message == "" {
			http.Error(w, "Message is required", http.StatusBadRequest)
			return
		}

		broker.Broadcast(taskId, message)
		fmt.Fprintf(w, "Message broadcasted to task %s", taskId)
	})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Example: Broadcast updates to task "123" every 5 seconds
				broker.Broadcast("123", fmt.Sprintf("Update for task 123 at %s", time.Now()))
				time.Sleep(5 * time.Second)
			}
		}
	}()
	if err := http.ListenAndServe(":8081", server); err != nil {
		fmt.Println("Could not start server", err)
	}
}
