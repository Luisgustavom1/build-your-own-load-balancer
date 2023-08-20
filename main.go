package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ServerPool struct {
  backends []*Backend
  current uint64
}

type Backend struct {
  URL *url.URL
  Alive bool
  mux sync.RWMutex
  ReverseProxy *httputil.ReverseProxy
}

type ContextKey int
const (
  Attempts ContextKey = iota
  Retry
)

func lb(writer http.ResponseWriter, request *http.Request) {
  attempts := GetAttemptsFromContext(request)
  if attempts > 3 {
    log.Printf("%s(%s) Max attempts reached, terminating\n", request.RemoteAddr, request.URL.Path)
    http.Error(writer, "Service not available", http.StatusServiceUnavailable)
    return
  }

  peer := serverPool.GetNextPeer()
  if peer != nil {
    peer.ReverseProxy.ServeHTTP(writer, request)
    return
  }
  http.Error(writer, "Service not available", http.StatusServiceUnavailable)
}

func GetAttemptsFromContext(r *http.Request) int {
  if attempts, ok := r.Context().Value(Attempts).(int); ok {
    return attempts
  }
  return 0
}

func GetRetryFromContext(r *http.Request) int {
  if retry, ok := r.Context().Value(Retry).(int); ok {
    return retry
  }
  return 0
}

func (s *ServerPool) GetNextPeer() *Backend {
  next := s.NextIndex()
  l := len(s.backends) + next

  // server pool as a loop
  for i := next; i < l; i++ {
    index := i % len(s.backends)

    if s.backends[index].IsAlive() {
      if i != next {
        atomic.StoreUint64(&s.current, uint64(index))
      }

      return s.backends[index]
    }
  }

  return nil
}

func (s *ServerPool) NextIndex() int {
  return int(atomic.AddUint64(&s.current, uint64(1)) % uint64(len(s.backends)))
}

func (s *ServerPool) HealthCheck() {
  for _, b := range s.backends {
    status := "up"
    alive := b.tryIfBackendIsAlive()
    b.SetAlive(alive)
    if !alive {
      status = "down"
    }
    log.Printf("%s [%s]\n", b.URL, status)
  }
}

func (s *ServerPool) MarkBackendStatus(backendUrl *url.URL, alive bool) {
	for _, b := range s.backends {
		if b.URL.String() == backendUrl.String() {
			b.SetAlive(alive)
			break
		}
	}
}

func (b *Backend) IsAlive() (alive bool) {
  b.mux.RLock()
  alive = b.Alive
  b.mux.RUnlock()
  return
}

func (b *Backend) tryIfBackendIsAlive() bool {
  timeout := 2 * time.Second
  conn, err := net.DialTimeout("tcp", b.URL.Host, timeout)
  if err != nil {
    log.Println("Site unreachable, error: ", err)
    return false 
  }
  defer conn.Close()
  return true
}

func (b *Backend) SetAlive(alive bool) {
  b.mux.Lock()
  b.Alive = alive
  b.mux.Unlock()
}

func healthCheck() {
  t := time.NewTicker(time.Second * 20)

  for {
    select {
    case <-t.C:
      log.Println("Starting health check...")
      serverPool.HealthCheck()
      log.Println("Health check completed")
    }
  }
}

var serverPool ServerPool

func createProxy(url *url.URL) *httputil.ReverseProxy {
  proxy := httputil.NewSingleHostReverseProxy(url)

  proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, e error) {
    log.Printf("[%s] %s\n", url.Host, e.Error())
    retries := GetRetryFromContext(request)

    if retries < 3 {
      select {
      case <-time.After(10 * time.Millisecond):
        ctx := context.WithValue(request.Context(), Retry, retries + 1)
        proxy.ServeHTTP(writer, request.WithContext(ctx))
      }
      return
    }

    serverPool.MarkBackendStatus(url, false)

    attempts := GetAttemptsFromContext(request)
    log.Printf("%s(%s) Attempting retry %d\n", request.RemoteAddr, request.URL.Path, attempts)
    ctx := context.WithValue(request.Context(), Attempts, attempts + 1)
    lb(writer, request.WithContext(ctx))
  }

  return proxy
}

func loadFlags(port *int, backends *string) {
  flag.IntVar(port, "port", 3000, "Port to server");
  flag.StringVar(backends, "backends", "", "Load balanced backends, use commas to separate")
  flag.Parse()
}

func main() {
  var port int
  var serverUrls string

  loadFlags(&port, &serverUrls)

  for _, u := range strings.Split(serverUrls, ",") {
    serverUrl, err := url.Parse(u)
    if err != nil {
      log.Fatal(err)
    }

    proxy := createProxy(serverUrl)

    serverPool.backends = append(serverPool.backends, &Backend{
      URL: serverUrl,
      Alive: true,
      ReverseProxy: proxy,
    })
    log.Printf("Configured server: %s\n", serverUrl)
  }

  server := http.Server{
   Addr: fmt.Sprintf(":%d", port), 
   Handler: http.HandlerFunc(lb),
  }

  go healthCheck()

  log.Printf("Load Balancer started at :%d\n", port)
  if err := server.ListenAndServe(); err != nil {
    log.Fatal(err)
  }
}