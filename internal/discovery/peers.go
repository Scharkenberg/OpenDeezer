package discovery

import (
	"net"
	"time"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/config"
	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
)

// ProbePeers lists devices for a picker: the LAN probe (minus selfPort) and then the
// configured peers, each asked /whoami concurrently within timeout and without the
// account id (any LAN host can answer a probe); a silent peer is listed by address. The
// list is never nil; the error is the probe's.
func ProbePeers(timeout time.Duration, selfPort int) ([]Device, error) {
	deadline := time.After(timeout)
	var bases, addrs []string // one per configured host:port
	seen := map[string]bool{}
	for _, p := range config.LoadPeers() {
		base, hp := config.NormalizePeer(p)
		if base == "" || seen[hp] {
			continue
		}
		seen[hp] = true
		bases, addrs = append(bases, base), append(addrs, hp)
	}
	// A buffered channel means a late responder never blocks its goroutine after
	// the collection stopped.
	results := make(chan Device, len(bases))
	for i := range bases {
		go func(base, hp string) {
			d := Device{Name: hp, Addr: hp}
			if who, err := control.NewClient(base, "", "").Whoami(); err == nil {
				if who.Name != "" {
					d.Name = who.Name
				}
				d.Client, d.Version = who.Client, who.Version
			}
			results <- d
		}(bases[i], addrs[i])
	}
	// Collect while the probe runs; peers that didn't answer get addr-only.
	peers := make(chan []Device, 1)
	go func() {
		got := map[string]Device{}
	collect:
		for range bases {
			select {
			case d := <-results:
				got[d.Addr] = d
			case <-deadline:
				break collect
			}
		}
		devs := make([]Device, len(addrs))
		for i, hp := range addrs {
			if d, ok := got[hp]; ok {
				devs[i] = d
			} else {
				devs[i] = Device{Name: hp, Addr: hp}
			}
		}
		peers <- devs
	}()

	devs, err := Discover(timeout, selfPort, config.PeerHostPorts()...)
	found := map[string]bool{}
	for _, d := range devs {
		found[d.Addr] = true
	}
	for _, d := range <-peers {
		if !found[d.Addr] {
			devs = append(devs, d)
		}
	}
	if devs == nil {
		devs = []Device{}
	}
	return devs, err
}

// LANIPv4 returns this machine's primary non-loopback IPv4 address for a LAN URL: the
// default route's source address, else the first adapter's, else "127.0.0.1".
func LANIPv4() string {
	// A UDP "connect" picks the route and sends nothing (TEST-NET-1 address).
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		ip := c.LocalAddr().(*net.UDPAddr).IP
		_ = c.Close()
		if !ip.IsLoopback() {
			return ip.String()
		}
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return "127.0.0.1"
}
