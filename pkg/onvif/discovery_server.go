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
}

// RegisterDevice registers an additional per-camera ONVIF device endpoint that
// will be included in WS-Discovery ProbeMatches responses alongside the main device.
// Must be called before StartDiscoveryServer. Returns the UUID for this device.
func RegisterDevice(port int) string {
	uuid := UUID()
	registeredDevices = append(registeredDevices, deviceEntry{uuid: uuid, port: port})
	return uuid
}

// StartDiscoveryServer listens for WS-Discovery Probe messages on UDP multicast
// 239.255.255.250:3702 and responds with a ProbeMatches reply containing one
// ProbeMatch entry per registered device (main device + any per-camera devices).
//
// apiPort is the HTTP port of the main go2rtc API server (e.g. 1984).
// All RegisterDevice calls must complete before calling this function.
func StartDiscoveryServer(apiPort int) error {
	// Snapshot all devices at start time. No mutex needed since all
	// RegisterDevice calls happen in Init() before this is called.
	allDevices := make([]deviceEntry, 0, 1+len(registeredDevices))
	allDevices = append(allDevices, deviceEntry{uuid: deviceUUID, port: apiPort})
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
		resp := buildProbeMatchResponse(msgID, devices)
		_, _ = conn.WriteToUDP(resp, from)
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

const discoveryScopes = "onvif://www.onvif.org/name/go2rtc " +
	"onvif://www.onvif.org/location/github " +
	"onvif://www.onvif.org/Profile/Streaming " +
	"onvif://www.onvif.org/type/Network_Video_Transmitter"

// buildProbeMatchResponse builds a WS-Discovery ProbeMatches SOAP envelope with
// one <ProbeMatch> element per registered device.
func buildProbeMatchResponse(relatesTo string, devices []deviceEntry) []byte {
	var matches strings.Builder
	for _, dev := range devices {
		xaddrs := buildXAddrsForPort(dev.port)
		if xaddrs == "" {
			continue
		}
		matches.WriteString(`      <d:ProbeMatch>
        <a:EndpointReference>
          <a:Address>urn:uuid:` + dev.uuid + `</a:Address>
        </a:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes>` + discoveryScopes + `</d:Scopes>
        <d:XAddrs>` + xaddrs + `</d:XAddrs>
        <d:MetadataVersion>1</d:MetadataVersion>
      </d:ProbeMatch>
`)
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
` + matches.String() + `    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`)
}
