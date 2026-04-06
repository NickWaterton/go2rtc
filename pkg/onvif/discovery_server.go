package onvif

import (
	"net"
	"strconv"
	"strings"

	"golang.org/x/net/ipv4"
)

var (
	discoveryMulticastGroup = net.IP{239, 255, 255, 250}
	discoveryMulticastAddr  = &net.UDPAddr{IP: discoveryMulticastGroup, Port: 3702}

	// deviceUUID is the stable endpoint reference for the main go2rtc ONVIF device.
	deviceUUID = UUID()

	// registeredDevices holds per-camera device entries added via RegisterDevice.
	// All registrations must happen before StartDiscoveryServer is called.
	registeredDevices []deviceEntry
)

type deviceEntry struct {
	uuid string
	port int
	name string  // human-readable name used in WS-Discovery scopes
	ip   net.IP  // specific source IP for this device; nil means use all interfaces
}

// RegisterDevice registers an additional per-camera ONVIF device endpoint that
// will be included in WS-Discovery ProbeMatches responses alongside the main device.
// ip may be nil (advertise on all interfaces) or a specific IP (for virtual-IP
// multi-camera setups where each camera has its own IP address).
// Must be called before StartDiscoveryServer. Returns the UUID for this device.
func RegisterDevice(port int, name string, ip net.IP) string {
	uuid := UUID()
	registeredDevices = append(registeredDevices, deviceEntry{uuid: uuid, port: port, name: name, ip: ip})
	return uuid
}

// StartDiscoveryServer listens for WS-Discovery Probe messages on UDP multicast
// 239.255.255.250:3702 and responds with a ProbeMatches message for each
// registered device.
//
// ONVIF clients (including Unifi Protect) deduplicate discovered devices by
// source IP address. Devices that share the host's IP therefore compete — only
// one is advertised via WS-Discovery (the first in the list). Devices with a
// dedicated virtual IP (deviceEntry.ip != nil) each get their own source IP and
// are all advertised independently, making them appear as distinct cameras.
//
// apiPort is the HTTP port of the main go2rtc API server (e.g. 1984).
// deviceName is the name advertised for the main device in WS-Discovery scopes.
// includeMain controls whether the main go2rtc device (on apiPort) is advertised
// in addition to the per-camera devices registered via RegisterDevice.
// All RegisterDevice calls must complete before calling this function.
func StartDiscoveryServer(apiPort int, deviceName string, includeMain bool) error {
	// Snapshot all devices at start time. No mutex needed since all
	// RegisterDevice calls happen in Init() before this is called.
	allDevices := make([]deviceEntry, 0, 1+len(registeredDevices))
	if includeMain {
		allDevices = append(allDevices, deviceEntry{uuid: deviceUUID, port: apiPort, name: deviceName})
	}
	allDevices = append(allDevices, registeredDevices...)

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 3702})
	if err != nil {
		return err
	}

	pc := ipv4.NewPacketConn(conn)
	_ = pc.SetMulticastLoopback(false)

	// Join the WS-Discovery multicast group on every eligible interface.
	ifaces, err := net.Interfaces()
	if err != nil {
		conn.Close()
		return err
	}
	for _, iface := range ifaces {
		if iface.Flags&(net.FlagUp|net.FlagMulticast) == net.FlagUp|net.FlagMulticast {
			_ = pc.JoinGroup(&iface, discoveryMulticastAddr)
		}
	}

	go discoveryLoop(conn, allDevices)
	return nil
}

func discoveryLoop(conn *net.UDPConn, devices []deviceEntry) {
	defer conn.Close()

	buf := make([]byte, 8192)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		body := buf[:n]

		if !isDiscoveryProbe(body) {
			continue
		}

		msgID := FindTagValue(body, "MessageID")

		// For devices with a dedicated IP, send from that IP so Unifi
		// Protect sees each as a distinct source and retains all of them.
		// For devices sharing the host's default IP, only the first is
		// sent — clients deduplicate by source IP so sending multiple
		// responses from the same IP causes a non-deterministic race.
		// Collect IPs dedicated to specific devices so default devices
		// don't advertise those IPs in their XAddrs.
		var dedicatedIPs []net.IP
		for _, dev := range devices {
			if dev.ip != nil {
				dedicatedIPs = append(dedicatedIPs, dev.ip)
			}
		}

		sentDefault := false
		for _, dev := range devices {
			resp := buildProbeMatchResponse(msgID, dev, dedicatedIPs)
			if resp == nil {
				continue
			}
			if dev.ip != nil {
				go sendFromIP(resp, dev.ip, from)
			} else {
				if sentDefault {
					continue
				}
				_, _ = conn.WriteToUDP(resp, from)
				sentDefault = true
			}
		}
	}
}

// sendFromIP sends data to addr using srcIP as the source address. This gives
// each virtual-IP device its own source IP in WS-Discovery responses, allowing
// ONVIF clients that deduplicate by source IP to see each device separately.
func sendFromIP(data []byte, srcIP net.IP, addr *net.UDPAddr) {
	c, err := net.DialUDP("udp4", &net.UDPAddr{IP: srcIP}, addr)
	if err != nil {
		return
	}
	defer c.Close()
	_, _ = c.Write(data)
}

// isDiscoveryProbe returns true if the message is a WS-Discovery Probe request.
func isDiscoveryProbe(b []byte) bool {
	action := FindTagValue(b, "Action")
	return strings.HasSuffix(strings.TrimSpace(action), "/Probe")
}

// buildXAddrsForDevice returns a space-separated list of ONVIF device service URLs.
// If ip is non-nil (virtual-IP device), only that IP is used. Otherwise one URL
// is built for each non-loopback IPv4 address on the host, excluding any IPs
// in excludeIPs (those are dedicated to other per-camera devices).
func buildXAddrsForDevice(ip net.IP, port int, excludeIPs []net.IP) string {
	p := strconv.Itoa(port)
	if ip != nil {
		return "http://" + ip.String() + ":" + p + PathDevice
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	var xaddrs []string
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() {
			continue
		}
		excluded := false
		for _, ex := range excludeIPs {
			if ip4.Equal(ex) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		xaddrs = append(xaddrs, "http://"+ip4.String()+":"+p+PathDevice)
	}
	return strings.Join(xaddrs, " ")
}

// buildDiscoveryScopes returns the WS-Discovery scope string for a device,
// embedding the device name (percent-encoded) in the name scope item.
func buildDiscoveryScopes(name string) string {
	encoded := strings.ReplaceAll(name, " ", "%20")
	return "onvif://www.onvif.org/name/" + encoded + " " +
		"onvif://www.onvif.org/location/github " +
		"onvif://www.onvif.org/Profile/Streaming " +
		"onvif://www.onvif.org/type/Network_Video_Transmitter"
}

// buildProbeMatchResponse builds a WS-Discovery ProbeMatches SOAP envelope
// containing a single ProbeMatch for the given device.
// excludeIPs is passed to buildXAddrsForDevice to prevent default devices from
// advertising IPs that belong to dedicated per-camera virtual interfaces.
func buildProbeMatchResponse(relatesTo string, dev deviceEntry, excludeIPs []net.IP) []byte {
	xaddrs := buildXAddrsForDevice(dev.ip, dev.port, excludeIPs)
	if xaddrs == "" {
		return nil
	}

	return []byte(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
  <s:Header>
    <a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</a:Action>
    <a:MessageID>urn:uuid:` + UUID() + `</a:MessageID>
    <a:RelatesTo>` + relatesTo + `</a:RelatesTo>
    <a:To>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:To>
  </s:Header>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address>urn:uuid:` + dev.uuid + `</a:Address>
        </a:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes>` + buildDiscoveryScopes(dev.name) + `</d:Scopes>
        <d:XAddrs>` + xaddrs + `</d:XAddrs>
        <d:MetadataVersion>1</d:MetadataVersion>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`)
}
