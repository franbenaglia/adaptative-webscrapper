package netguard

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestCheckAddr(t *testing.T) {
	forbidden := []string{
		"127.0.0.1", "127.8.8.8", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.10", "169.254.169.254",
		"fe80::1", "fc00::1", "fd12:3456::1", "0.0.0.0", "::", "100.64.0.1", "224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::", "198.18.0.1", "240.0.0.1", "0.1.2.3",
	}
	for _, raw := range forbidden {
		if err := (Policy{}).CheckAddr(netip.MustParseAddr(raw)); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s debería estar prohibida: %v", raw, err)
		}
	}
	for _, raw := range []string{"93.184.216.34", "8.8.8.8", "2606:2800:220:1:248:1893:25c8:1946", "172.32.0.1", "100.128.0.1"} {
		if err := (Policy{}).CheckAddr(netip.MustParseAddr(raw)); err != nil {
			t.Errorf("%s debería permitirse: %v", raw, err)
		}
	}
	if err := (Policy{}).CheckAddr(netip.Addr{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("una dirección vacía debe prohibirse: %v", err)
	}

	dev := Policy{AllowPrivate: true, Blocked: []netip.Prefix{netip.MustParsePrefix("127.0.0.2/32")}}
	if err := dev.CheckAddr(netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Errorf("con AllowPrivate, loopback se permite: %v", err)
	}
	if err := dev.CheckAddr(netip.MustParseAddr("127.0.0.2")); !errors.Is(err, ErrForbidden) {
		t.Errorf("Blocked manda sobre AllowPrivate: %v", err)
	}
}

func TestResolveAndCheckURL(t *testing.T) {
	ctx := context.Background()
	if _, err := (Policy{}).Resolve(ctx, "localhost"); !errors.Is(err, ErrForbidden) {
		t.Errorf("localhost debe resolverse y prohibirse: %v", err)
	}
	if addrs, err := (Policy{AllowPrivate: true}).Resolve(ctx, "localhost"); err != nil || len(addrs) == 0 {
		t.Errorf("localhost con AllowPrivate = %v, %v", addrs, err)
	}
	for _, raw := range []string{
		"http://127.0.0.1:8080/", "http://[::1]/", "http://localhost/admin", "http://169.254.169.254/latest/meta-data/",
		"http://192.168.0.1/", "file:///etc/passwd", "ftp://93.184.216.34/", "javascript:alert(1)", "http://%zz/",
	} {
		if err := (Policy{}).CheckURL(ctx, raw); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s debería rechazarse con ErrForbidden: %v", raw, err)
		}
	}
	if err := (Policy{}).CheckURL(ctx, "https://93.184.216.34/pagina"); err != nil {
		t.Errorf("una IP pública debe permitirse: %v", err)
	}
	// Un nombre que no resuelve se rechaza, aunque no sea por la política.
	if err := (Policy{}).CheckURL(ctx, "http://no-existe.invalid/"); err == nil {
		t.Error("un host sin DNS debe fallar")
	}
}

func proxyClient(t *testing.T, policy Policy, maxBytes int64) *http.Client {
	t.Helper()
	proxy, err := StartProxy(policy, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	proxyURL, _ := url.Parse(proxy.URL())
	return &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // certificado de httptest
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func TestProxyBlocksPrivateDestinations(t *testing.T) {
	hits := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits++; _, _ = w.Write([]byte("secreto interno")) })
	plain := httptest.NewServer(handler)
	defer plain.Close()
	secure := httptest.NewTLSServer(handler)
	defer secure.Close()
	client := proxyClient(t, Policy{}, 0)

	resp, err := client.Get(plain.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get(BlockedHeader) == "" || strings.Contains(string(body), "secreto") {
		t.Errorf("HTTP a loopback = %d %q", resp.StatusCode, body)
	}
	// En HTTPS el rechazo ocurre en el CONNECT, antes de cualquier intercambio.
	if _, err := client.Get(secure.URL); err == nil || !strings.Contains(err.Error(), "Forbidden") {
		t.Errorf("HTTPS a loopback debería fallar en el túnel: %v", err)
	}
	if hits != 0 {
		t.Errorf("el servidor interno recibió %d peticiones", hits)
	}
}

func TestProxyForwardsAllowedTraffic(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Connection") != "" {
			t.Error("las cabeceras de salto no deben reenviarse")
		}
		w.Header().Set("X-Test", "ok")
		_, _ = w.Write([]byte("hola " + r.URL.Path + " " + r.Header.Get("X-Client")))
	})
	plain := httptest.NewServer(handler)
	defer plain.Close()
	secure := httptest.NewTLSServer(handler)
	defer secure.Close()
	client := proxyClient(t, Policy{AllowPrivate: true}, 0)

	for _, base := range []string{plain.URL, secure.URL} {
		req, _ := http.NewRequest("GET", base+"/ruta", nil)
		req.Header.Set("X-Client", "navegador")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || string(body) != "hola /ruta navegador" || resp.Header.Get("X-Test") != "ok" {
			t.Errorf("%s = %d %q", base, resp.StatusCode, body)
		}
	}
}

// Un sitio permitido no puede usar una redirección para alcanzar uno prohibido.
func TestProxyChecksRedirectTargets(t *testing.T) {
	internalHits := 0
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { internalHits++ }))
	defer internal.Close()
	target := strings.Replace(internal.URL, "127.0.0.1", "127.0.0.2", 1)
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target+"/metadatos", http.StatusFound)
	}))
	defer public.Close()
	policy := Policy{AllowPrivate: true, Blocked: []netip.Prefix{netip.MustParsePrefix("127.0.0.2/32")}}
	client := proxyClient(t, policy, 0)

	first, err := client.Get(public.URL)
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusFound {
		t.Fatalf("el proxy no debe seguir redirecciones por su cuenta: %d", first.StatusCode)
	}
	second, err := client.Get(first.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusForbidden || second.Header.Get(BlockedHeader) == "" || internalHits != 0 {
		t.Errorf("destino de la redirección = %d, peticiones internas = %d", second.StatusCode, internalHits)
	}
}

func TestProxyLimitsResponseSize(t *testing.T) {
	big := strings.Repeat("x", 10_000)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(big)) })
	plain := httptest.NewServer(handler)
	defer plain.Close()
	client := proxyClient(t, Policy{AllowPrivate: true}, 1000)

	resp, err := client.Get(plain.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) > 1000 {
		t.Errorf("se entregaron %d bytes con un límite de 1000", len(body))
	}
}

func TestProxyRejectsNonProxyRequests(t *testing.T) {
	proxy, err := StartProxy(Policy{AllowPrivate: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	resp, err := http.Get(proxy.URL() + "/directo")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("petición directa al proxy = %d", resp.StatusCode)
	}
}
