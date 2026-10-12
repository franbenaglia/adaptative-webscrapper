package netguard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// BlockedHeader marca las respuestas 403 generadas por el proxy, para
// distinguirlas de un 403 del sitio.
const BlockedHeader = "X-Adaptive-Scraper-Blocked"

// hopHeaders son cabeceras de un solo salto que un proxy no reenvía.
var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// Proxy es un proxy HTTP local que aplica una Policy a cada conexión. El
// navegador se configura para enviarle todo: páginas, subrecursos,
// redirecciones y WebSockets. Así ninguna petición escapa al filtro.
type Proxy struct {
	policy    Policy
	listener  net.Listener
	server    *http.Server
	transport *http.Transport
	// maxBytes limita lo que se entrega al navegador por conexión; 0 es sin límite.
	maxBytes int64
}

// StartProxy arranca el proxy en un puerto libre de loopback.
func StartProxy(policy Policy, maxBytesPerConnection int64) (*Proxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		policy:   policy,
		listener: listener,
		maxBytes: maxBytesPerConnection,
		transport: &http.Transport{
			DialContext:           policy.DialContext,
			DisableCompression:    true,
			ResponseHeaderTimeout: time.Minute,
			IdleConnTimeout:       30 * time.Second,
		},
	}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 30 * time.Second}
	go func() { _ = p.server.Serve(listener) }()
	return p, nil
}

// URL es la dirección que debe configurarse como proxy del navegador.
func (p *Proxy) URL() string { return "http://" + p.listener.Addr().String() }

func (p *Proxy) Close() error {
	p.transport.CloseIdleConnections()
	return p.server.Close()
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	if !r.URL.IsAbs() || r.URL.Scheme != "http" {
		http.Error(w, "petición de proxy no admitida", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, name := range hopHeaders {
		out.Header.Del(name)
	}
	// RoundTrip no sigue redirecciones: el navegador las pide de nuevo al
	// proxy y vuelven a pasar por la política.
	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		p.fail(w, err)
		return
	}
	defer resp.Body.Close()
	for name, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
	for _, name := range hopHeaders {
		w.Header().Del(name)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, p.limit(resp.Body))
}

// tunnel atiende CONNECT (HTTPS y WebSockets): conecta con el destino ya
// comprobado y copia bytes en ambos sentidos.
func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	upstream, err := p.policy.DialContext(ctx, "tcp", r.Host)
	cancel()
	if err != nil {
		p.fail(w, err)
		return
	}
	defer upstream.Close()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy sin soporte de túnel", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(upstream, buffered) // incluye lo ya leído del cliente
		closeWrite(upstream)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, p.limit(upstream))
		// Al agotar el límite o cerrarse el destino se corta también la subida.
		_ = client.Close()
		_ = upstream.Close()
	}()
	wg.Wait()
}

func (p *Proxy) limit(r io.Reader) io.Reader {
	if p.maxBytes <= 0 {
		return r
	}
	return io.LimitReader(r, p.maxBytes)
}

func (p *Proxy) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrForbidden) {
		w.Header().Set(BlockedHeader, "network-policy")
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	http.Error(w, "no se pudo conectar con el destino", http.StatusBadGateway)
}

func closeWrite(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
}
