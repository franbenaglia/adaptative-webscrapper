// Package netguard decide a qué direcciones de red puede llegar el navegador
// y lo hace cumplir con un proxy local por el que pasa todo su tráfico.
//
// La URL la aporta el usuario (o una receta), así que sin este filtro el
// navegador podría usarse para leer servicios internos de la máquina o de la
// red local (SSRF).
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
)

// ErrForbidden indica que el destino está fuera de lo permitido.
var ErrForbidden = errors.New("destino no permitido por la política de red")

// reserved son rangos no enrutables o especiales que las comprobaciones de
// netip.Addr no cubren.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), // pruebas de rendimiento
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64: envuelve direcciones IPv4
	netip.MustParsePrefix("2002::/16"),    // 6to4: ídem
}

// Policy describe los destinos permitidos. El valor cero solo permite
// direcciones públicas.
type Policy struct {
	// AllowPrivate permite loopback, redes privadas y link-local. Solo para
	// desarrollo y para el sitio de demostración.
	AllowPrivate bool
	// Blocked son rangos prohibidos siempre, incluso con AllowPrivate.
	Blocked []netip.Prefix
	// Resolver permite sustituir la resolución DNS; nil usa la del sistema.
	Resolver *net.Resolver
}

// CheckAddr comprueba una dirección IP.
func (p Policy) CheckAddr(addr netip.Addr) error {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return fmt.Errorf("%w: dirección inválida", ErrForbidden)
	}
	for _, prefix := range p.Blocked {
		if prefix.Contains(addr) {
			return fmt.Errorf("%w: %s está en un rango bloqueado", ErrForbidden, addr)
		}
	}
	if p.AllowPrivate {
		return nil
	}
	private := addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified()
	for _, prefix := range reserved {
		private = private || prefix.Contains(addr)
	}
	if private {
		return fmt.Errorf("%w: %s es una dirección local, privada o reservada (se permite con APP_ALLOW_PRIVATE_NETWORKS=true)", ErrForbidden, addr)
	}
	return nil
}

// Resolve devuelve las direcciones de host si todas están permitidas. Basta
// una prohibida para rechazar el host: evita que un nombre con varias
// direcciones cuele una interna.
func (p Policy) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, p.CheckAddr(addr)
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolver %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolver %s: sin direcciones", host)
	}
	for _, addr := range addrs {
		if err := p.CheckAddr(addr); err != nil {
			return nil, fmt.Errorf("%s: %w", host, err)
		}
	}
	return addrs, nil
}

// CheckURL comprueba el esquema y el host de una URL antes de navegar.
func (p Policy) CheckURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: URL mal formada", ErrForbidden)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: esquema %q", ErrForbidden, u.Scheme)
	}
	_, err = p.Resolve(ctx, u.Hostname())
	return err
}

// DialContext resuelve, comprueba y conecta a la dirección ya comprobada, de
// modo que no hay una segunda resolución DNS que pueda dar otra respuesta
// (DNS rebinding).
func (p Policy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	addrs, err := p.Resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	var lastErr error
	for _, addr := range addrs {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
