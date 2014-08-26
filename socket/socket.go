package socket

import (
	"io"
	"net"
	"os"
	"syscall"
)

func CreateTCPSocket(proto, addr string) (net.Listener, error) {
	var err error
	var laddr *net.TCPAddr

	laddr, err = net.ResolveTCPAddr(proto, addr)
	if err != nil {
		return nil, err
	}

	family, ipv6only := favoriteTCPAddrFamily(proto, laddr, "listen")

	var socketAddr syscall.Sockaddr
	if socketAddr, err = ipToSockaddr(family, laddr.IP, laddr.Port, laddr.Zone); err != nil {
		panic(err)
		return nil, err
	}

	var s int
	if s, err = sysSocket(family, syscall.SOCK_STREAM, 0); err != nil {
		return nil, err
	}

	if err = setDefaultSockopts(s, family, syscall.SOCK_STREAM, ipv6only); err != nil {
		closesocket(s)
		return nil, err
	}

	if err = setDefaultListenerSockopts(s); err != nil {
		closesocket(s)
		return nil, err
	}

	if err = syscall.SetsockoptInt(s, syscall.SOL_SOCKET, SO_REUSEPORT, 1); err != nil {
		closesocket(s)
		panic(err)
		return nil, err
	}

	if err = syscall.Bind(s, socketAddr); err != nil {
		closesocket(s)
		return nil, err
	}

	if err = syscall.Listen(s, maxListenerBacklog()); err != nil {
		closesocket(s)
		return nil, err
	}

	file := os.NewFile(uintptr(s), "listener-"+laddr.String())
	defer file.Close()

	var socketListener net.Listener
	if socketListener, err = net.FileListener(file); err != nil {
		return nil, err
	}

	return socketListener, nil

}

// copied from net/fd_unix.go

func closesocket(s int) error {
	return syscall.Close(s)
}

// copied from net/ipsock_posix.go

func favoriteTCPAddrFamily(net string, laddr *net.TCPAddr, mode string) (family int, ipv6only bool) {
	switch net[len(net)-1] {
	case '4':
		return syscall.AF_INET, false
	case '6':
		return syscall.AF_INET6, true
	}

	if mode == "listen" && (laddr == nil || isTCPWildcard(laddr)) {
		if supportsIPv4map {
			return syscall.AF_INET6, false
		}
		if laddr == nil {
			return syscall.AF_INET, false
		}
		return familyTCP(laddr), false
	}

	if laddr == nil || familyTCP(laddr) == syscall.AF_INET {
		return syscall.AF_INET, false
	}
	return syscall.AF_INET6, false
}

func ipToSockaddr(family int, ip net.IP, port int, zone string) (syscall.Sockaddr, error) {
	switch family {
	case syscall.AF_INET:
		if len(ip) == 0 {
			ip = net.IPv4zero
		}
		if ip = ip.To4(); ip == nil {
			return nil, net.InvalidAddrError("non-IPv4 address")
		}
		sa := new(syscall.SockaddrInet4)
		for i := 0; i < net.IPv4len; i++ {
			sa.Addr[i] = ip[i]
		}
		sa.Port = port
		return sa, nil
	case syscall.AF_INET6:
		if len(ip) == 0 {
			ip = net.IPv6zero
		}
		// IPv4 callers use 0.0.0.0 to mean "announce on any available address".
		// In IPv6 mode, Linux treats that as meaning "announce on 0.0.0.0",
		// which it refuses to do.  Rewrite to the IPv6 unspecified address.
		if ip.Equal(net.IPv4zero) {
			ip = net.IPv6zero
		}
		if ip = ip.To16(); ip == nil {
			return nil, net.InvalidAddrError("non-IPv6 address")
		}
		sa := new(syscall.SockaddrInet6)
		for i := 0; i < net.IPv6len; i++ {
			sa.Addr[i] = ip[i]
		}
		sa.Port = port
		sa.ZoneId = uint32(zoneToInt(zone))
		return sa, nil
	}
	return nil, net.InvalidAddrError("unexpected socket family")
}

func probeIPv4Stack() bool {
	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	switch err {
	case syscall.EAFNOSUPPORT, syscall.EPROTONOSUPPORT:
		return false
	case nil:
		closesocket(s)
	}
	return true
}

func probeIPv6Stack() (supportsIPv6, supportsIPv4map bool) {
	var probes = []struct {
		laddr net.TCPAddr
		value int
		ok    bool
	}{
		// IPv6 communication capability
		{laddr: net.TCPAddr{IP: net.ParseIP("::1")}, value: 1},
		// IPv6 IPv4-mapped address communication capability
		{laddr: net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, value: 0},
	}

	for i := range probes {
		s, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
		if err != nil {
			continue
		}
		defer closesocket(s)
		syscall.SetsockoptInt(s, syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, probes[i].value)
		sa, err := sockaddrTCP(&probes[i].laddr, syscall.AF_INET6)
		if err != nil {
			continue
		}
		if err := syscall.Bind(s, sa); err != nil {
			continue
		}
		probes[i].ok = true
	}

	return probes[0].ok, probes[1].ok
}

// copied from net/ipsock.go

var (
	// supportsIPv4 reports whether the platform supports IPv4
	// networking functionality.
	supportsIPv4 bool

	// supportsIPv6 reports whether the platform supports IPv6
	// networking functionality.
	supportsIPv6 bool

	// supportsIPv4map reports whether the platform supports
	// mapping an IPv4 address inside an IPv6 address at transport
	// layer protocols.  See RFC 4291, RFC 4038 and RFC 3493.
	supportsIPv4map bool
)

func init() {
	supportsIPv4 = probeIPv4Stack()
	supportsIPv6, supportsIPv4map = probeIPv6Stack()
}

func zoneToInt(zone string) int {
	if zone == "" {
		return 0
	}
	if ifi, err := net.InterfaceByName(zone); err == nil {
		return ifi.Index
	}
	n, _, _ := dtoi(zone, 0)
	return n
}

// copied from net/parse.go

type file struct {
	file  *os.File
	data  []byte
	atEOF bool
}

func (f *file) close() { f.file.Close() }

func (f *file) getLineFromData() (s string, ok bool) {
	data := f.data
	i := 0
	for i = 0; i < len(data); i++ {
		if data[i] == '\n' {
			s = string(data[0:i])
			ok = true
			// move data
			i++
			n := len(data) - i
			copy(data[0:], data[i:])
			f.data = data[0:n]
			return
		}
	}
	if f.atEOF && len(f.data) > 0 {
		// EOF, return all we have
		s = string(data)
		f.data = f.data[0:0]
		ok = true
	}
	return
}

func (f *file) readLine() (s string, ok bool) {
	if s, ok = f.getLineFromData(); ok {
		return
	}
	if len(f.data) < cap(f.data) {
		ln := len(f.data)
		n, err := io.ReadFull(f.file, f.data[ln:cap(f.data)])
		if n >= 0 {
			f.data = f.data[0 : ln+n]
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			f.atEOF = true
		}
	}
	s, ok = f.getLineFromData()
	return
}

func open(name string) (*file, error) {
	fd, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return &file{fd, make([]byte, 0, os.Getpagesize()), false}, nil
}

// Bigger than we need, not too big to worry about overflow
const big = 0xFFFFFF

func dtoi(s string, i0 int) (n int, i int, ok bool) {
	n = 0
	for i = i0; i < len(s) && '0' <= s[i] && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
		if n >= big {
			return 0, i, false
		}
	}
	if i == i0 {
		return 0, i, false
	}
	return n, i, true
}

// copied from net/sock_cloexec.go

func sysSocket(family, sotype, proto int) (int, error) {
	s, err := syscall.Socket(family, sotype|SOCK_NONBLOCK|SOCK_CLOEXEC, proto)
	// On Linux the SOCK_NONBLOCK and SOCK_CLOEXEC flags were
	// introduced in 2.6.27 kernel and on FreeBSD both flags were
	// introduced in 10 kernel. If we get an EINVAL error on Linux
	// or EPROTONOSUPPORT error on FreeBSD, fall back to using
	// socket without them.
	if err == nil || (err != syscall.EPROTONOSUPPORT && err != syscall.EINVAL) {
		return s, err
	}

	// See ../syscall/exec_unix.go for description of ForkLock.
	syscall.ForkLock.RLock()
	s, err = syscall.Socket(family, sotype, proto)
	if err == nil {
		syscall.CloseOnExec(s)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return -1, err
	}
	if err = syscall.SetNonblock(s, true); err != nil {
		syscall.Close(s)
		return -1, err
	}
	return s, nil
}

// copied from net/sockopt_linux.go

func setDefaultSockopts(s, family, sotype int, ipv6only bool) error {
	if family == syscall.AF_INET6 && sotype != syscall.SOCK_RAW {
		// Allow both IP versions even if the OS default
		// is otherwise.  Note that some operating systems
		// never admit this option.
		syscall.SetsockoptInt(s, syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, boolint(ipv6only))
	}
	// Allow broadcast.
	return os.NewSyscallError("setsockopt", syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1))
}

func setDefaultListenerSockopts(s int) error {
	// Allow reuse of recently-used addresses.
	// Set SO_REUSEPORT too.
	return os.NewSyscallError("setsockopt", syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1))
}

// copied from net/sockopt_posix.go

func boolint(b bool) int {
	if b {
		return 1
	}
	return 0
}

// copied from net/tcpsock_posix.go

func isTCPWildcard(a *net.TCPAddr) bool {
	if a == nil || a.IP == nil {
		return true
	}
	return a.IP.IsUnspecified()
}

func familyTCP(a *net.TCPAddr) int {
	if a == nil || len(a.IP) <= net.IPv4len {
		return syscall.AF_INET
	}
	if a.IP.To4() != nil {
		return syscall.AF_INET
	}
	return syscall.AF_INET6
}

func sockaddrTCP(a *net.TCPAddr, family int) (syscall.Sockaddr, error) {
	if a == nil {
		return nil, nil
	}
	return ipToSockaddr(family, a.IP, a.Port, a.Zone)
}
