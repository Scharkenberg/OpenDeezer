// Package discovery finds OpenDeezer instances on the LAN (Connect): each instance with
// the control API answers a UDP probe (multicast 239.255.42.99:7655, broadcast fallback)
// with its name, client and version, only to private sources, never the user id.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
)

const (
	// Port is the UDP port the responder listens on.
	Port = 7655

	probeMagic = "OPENDEEZER_DISCOVER_v1"
	replyMagic = "OPENDEEZER_DEVICE_v1"
	maxPacket  = 512
)

// groupIP is the admin-scoped IPv4 multicast group used for probes.
var groupIP = net.IPv4(239, 255, 42, 99)

// Device is a discovered OpenDeezer instance.
type Device struct {
	Name    string `json:"name"`    // account display name (not the user id)
	Addr    string `json:"addr"`    // control API host:port
	Client  string `json:"client"`  // client/platform id (tui, macos, …)
	Version string `json:"version"` // OpenDeezer version
}

// Info is the advertised identity (read per request, so re-login is reflected).
type Info struct {
	Name    string
	Client  string
	Version string
}

// reply is the JSON payload (after replyMagic) a responder sends.
type reply struct {
	Magic   string `json:"magic"`
	Name    string `json:"name"`
	Port    int    `json:"port"` // control API port; host is taken from the reply source IP
	Client  string `json:"client"`
	Version string `json:"version"`
}

// Responder advertises this instance until Close.
type Responder struct {
	mu          sync.Mutex
	conn        *net.UDPConn
	closed      bool
	info        func() Info
	controlPort int
	done        chan struct{}
	wg          sync.WaitGroup
	baseIPs     []string // IP baseline captured in Advertise (before the supervisor goroutine starts)
}

// bindHost is the address both sockets bind: every interface. Tests bind loopback,
// which the OS firewall never asks about.
var bindHost = ""

// listenPort is the UDP port the responder binds: Port. Tests use a free
// port the OS picked, so a running OpenDeezer does not answer them.
var listenPort = Port

// Advertise starts the discovery responder on UDP :Port (reuse-port, so several
// instances on one host coexist) and joins the multicast group on every capable
// interface. controlPort is the control API port to advertise.
func Advertise(info func() Info, controlPort int) (*Responder, error) {
	r := &Responder{
		info:        info,
		controlPort: controlPort,
		done:        make(chan struct{}),
	}
	if err := r.rebind(); err != nil {
		return nil, err
	}
	// Capture the IP baseline before starting the supervisor; read inside the goroutine,
	// it could already include a change and never trigger a rebind.
	r.baseIPs = getLocalIPsList()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.supervisor()
	}()
	return r, nil
}

func (r *Responder) rebind() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	if r.conn != nil {
		_ = r.conn.Close()
	}

	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error { return setReusePort(c) }}
	pc, err := lc.ListenPacket(context.Background(), "udp4", bindHost+":"+strconv.Itoa(listenPort))
	if err != nil {
		return err
	}
	conn := pc.(*net.UDPConn)
	p := ipv4.NewPacketConn(conn)
	for _, ifi := range multicastInterfaces() {
		_ = p.JoinGroup(&ifi, &net.UDPAddr{IP: groupIP})
	}
	r.conn = conn

	r.wg.Add(1)
	go r.serve(conn)
	return nil
}

func (r *Responder) serve(conn *net.UDPConn) {
	defer r.wg.Done()
	buf := make([]byte, maxPacket)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if retryableRead(err) {
				continue
			}
			return // closed, or a socket that cannot recover
		}
		if string(buf[:n]) != probeMagic || !isLAN(src.IP) {
			continue
		}
		in := r.info()
		b, _ := json.Marshal(reply{
			Magic: replyMagic, Name: in.Name, Port: r.controlPort,
			Client: in.Client, Version: in.Version,
		})
		_, _ = conn.WriteToUDP(b, src)
	}
}

var supervisorInterval = 30 * time.Second

func (r *Responder) supervisor() {
	ticker := time.NewTicker(supervisorInterval)
	defer ticker.Stop()

	lastIPs := r.baseIPs // baseline captured in Advertise (avoids the scheduling race)
	for {
		select {
		case <-ticker.C:
			currentIPs := getLocalIPsList()
			if ipsChanged(lastIPs, currentIPs) {
				if err := r.rebind(); err == nil {
					lastIPs = currentIPs
				}
			}
		case <-r.done:
			return
		}
	}
}

// Close stops the responder.
func (r *Responder) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.done)
	if r.conn != nil {
		_ = r.conn.Close()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// Discover multicasts a probe (broadcast fallback), unicasts staticPeers, and collects
// replies until timeout. Replies from a local address on selfPort (our own responder)
// are dropped.
func Discover(timeout time.Duration, selfPort int, staticPeers ...string) ([]Device, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error { return setBroadcast(c) }}
	pc, err := lc.ListenPacket(context.Background(), "udp4", bindHost+":0") // ephemeral
	if err != nil {
		return nil, err
	}
	conn := pc.(*net.UDPConn)
	defer func() { _ = conn.Close() }()

	probe := []byte(probeMagic)
	group := &net.UDPAddr{IP: groupIP, Port: Port}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	// Probes go out from a goroutine at once and every 2 s, so resolving a hostname peer
	// never delays the collection below, which the deadline bounds.
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		sendProbes(conn, probe, group, staticPeers)
		for {
			select {
			case <-ticker.C:
				sendProbes(conn, probe, group, staticPeers)
			case <-done:
				return
			}
		}
	}()

	locals := localIPs()
	seen := map[string]bool{}
	var out []Device
	buf := make([]byte, maxPacket)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if ne, ok := err.(net.Error); ok && ne.Timeout() || errors.Is(err, net.ErrClosed) {
			break
		}
		if err != nil {
			continue // an oversized datagram or an ICMP reset from a unicast peer
		}
		var rep reply
		// Bound the port so a forged reply can't produce a bad address.
		if json.Unmarshal(buf[:n], &rep) != nil || rep.Magic != replyMagic || rep.Port <= 0 || rep.Port > 65535 {
			continue
		}
		// Skip our own responder (same control port on one of our local addresses).
		if selfPort != 0 && rep.Port == selfPort && (src.IP.IsLoopback() || locals[src.IP.String()]) {
			continue
		}
		addr := net.JoinHostPort(src.IP.String(), strconv.Itoa(rep.Port))
		if seen[addr] {
			continue
		}
		seen[addr] = true
		name := rep.Name
		if name == "" {
			name = addr
		}
		out = append(out, Device{Name: name, Addr: addr, Client: rep.Client, Version: rep.Version})
	}
	return out, nil
}

func sendProbes(conn *net.UDPConn, probe []byte, group *net.UDPAddr, staticPeers []string) {
	p := ipv4.NewPacketConn(conn)
	_ = p.SetMulticastTTL(4)

	// Multicast the probe out of each capable interface.
	ifaces := multicastInterfaces()
	for i := range ifaces {
		if p.SetMulticastInterface(&ifaces[i]) == nil {
			_, _ = conn.WriteToUDP(probe, group)
		}
	}
	if len(ifaces) == 0 {
		_, _ = conn.WriteToUDP(probe, group)
	}
	// Fallbacks for networks that filter multicast: per-interface directed broadcast
	// (e.g. 192.168.1.255), which crosses a subnet more reliably, plus limited broadcast.
	for _, bc := range broadcastAddrs() {
		_, _ = conn.WriteToUDP(probe, &net.UDPAddr{IP: bc, Port: Port})
	}
	_, _ = conn.WriteToUDP(probe, &net.UDPAddr{IP: net.IPv4bcast, Port: Port})

	// Unicast static peers in parallel
	for _, peer := range staticPeers {
		host, _, err := net.SplitHostPort(peer)
		if err != nil {
			host = peer
		}
		addrStr := net.JoinHostPort(host, strconv.Itoa(Port))
		if udpAddr, err := net.ResolveUDPAddr("udp4", addrStr); err == nil {
			_, _ = conn.WriteToUDP(probe, udpAddr)
		}
	}
}

var getLocalIPsList = func() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			ips = append(ips, ipnet.IP.String())
		}
	}
	return ips
}

func ipsChanged(oldIPs, newIPs []string) bool {
	if len(oldIPs) != len(newIPs) {
		return true
	}
	m := make(map[string]bool)
	for _, ip := range oldIPs {
		m[ip] = true
	}
	for _, ip := range newIPs {
		if !m[ip] {
			return true
		}
	}
	return false
}

// broadcastAddrs returns the IPv4 directed-broadcast address of each non-loopback
// interface (host bits set), e.g. 192.168.1.255 for 192.168.1.7/24.
func broadcastAddrs() []net.IP {
	var out []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() {
			continue
		}
		mask := ipnet.Mask
		if len(mask) != 4 {
			continue
		}
		bc := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			bc[i] = ip4[i] | ^mask[i]
		}
		out = append(out, bc)
	}
	return out
}

// localIPs is the set of this machine's interface IPs (for self-filtering).
func localIPs() map[string]bool {
	m := map[string]bool{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return m
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			m[ipnet.IP.String()] = true
		}
	}
	return m
}

// multicastInterfaces returns up, multicast-capable interfaces.
func multicastInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.Interface
	for _, ifi := range all {
		if ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagMulticast != 0 {
			out = append(out, ifi)
		}
	}
	return out
}

// isLAN reports whether ip is a private/loopback/link-local or carrier-grade
// NAT (100.64.0.0/10, Tailscale) address: safe to answer; public sources are
// refused to avoid being a reflection amplifier.
func isLAN(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
