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
	name string // human-readable name used in WS-Discovery scopes
}

// RegisterDevice registers an additional per-camera ONVIF device endpoint that
// will be included in WS-Discovery ProbeMatches responses alongside the main device.
// Must be called before StartDiscoveryServer. Returns the UUID for this device.
func RegisterDevice(port int, name string) string {
	uuid := UUID()
	registeredDevices = append(registeredDevices, deviceEntry{uuid: uuid, port: port, name: name})
	return uuid
}

// StartDiscoveryServer listens for WS-Discovery Probe messages on UDP multicast
// 239.255.255.250:3702. For each Probe it sends one ProbeMatches response per
// registered device (separate UDP packets) for maximum client compatibility —
// some ONVIF clients (including Unifi Protect) only process the first ProbeMatch
// in a combined response.
//
// apiPort is the HTTP port of the main go2rtc API server (e.g. 1984).
// deviceName is the name advertised for the main device in WS-Discovery scopes.
// All RegisterDevice calls must complete before calling this function.
func StartDiscoveryServer(apiPort int, deviceName string) error {
	// Snapshot all devices at start time. No mutex needed since all
	// RegisterDevice calls happen in Init() before this is called.
	allDevices := make([]deviceEntry, 0, 1+len(registeredDevices))
	allDevices = append(allDevices, deviceEntry{uuid: deviceUUID, port: apiPort, name: deviceName})
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

		// Send one separate ProbeMatches response per device.
		// Unifi Protect (and many other clients) only process the first
		// ProbeMatch in a combined multi-ProbeMatch response.
		for _, dev := range devices {
			resp := buildProbeMatchResponse(msgID, dev)
			_, _ = conn.WriteToUDP(resp, from)
		}
	}
}

// isDiscoveryProbe returns true if the message is a WS-Discovery Probe request.
func isDiscoveryProbe(b []byte) bool {
	action := FindTagValue(b, "Action")
	return strings.HasSuffix(strings.TrimSpace(action), "/Probe")
}

// buildXAddrsForPort returns a space-separated list of ONVIF device service URLs,
// one for each non-loopback IPv4 address on this host, using the given port.
func buildXAddrsForPort(port int) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	p := strconv.Itoa(port)
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
func buildProbeMatchResponse(relatesTo string, dev deviceEntry) []byte {
	xaddrs := buildXAddrsForPort(dev.port)
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
