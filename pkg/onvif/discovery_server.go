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

	// deviceUUID is generated once per run and used as the stable endpoint
	// reference address in WS-Discovery ProbeMatches responses.
	deviceUUID = UUID()
)

// StartDiscoveryServer listens for WS-Discovery Probe messages on UDP multicast
// 239.255.255.250:3702 and responds with ProbeMatches so ONVIF clients such as
// Unifi Protect can auto-discover this go2rtc instance.
//
// apiPort is the HTTP port go2rtc is listening on (e.g. 1984).
func StartDiscoveryServer(apiPort int) error {
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

	go discoveryLoop(conn, apiPort)
	return nil
}

func discoveryLoop(conn *net.UDPConn, apiPort int) {
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

		xaddrs := buildXAddrs(apiPort)
		if xaddrs == "" {
			continue
		}

		msgID := FindTagValue(body, "MessageID")
		resp := buildProbeMatchResponse(msgID, xaddrs)
		_, _ = conn.WriteToUDP(resp, from)
	}
}

// isDiscoveryProbe returns true if the message is a WS-Discovery Probe request.
// We identify it by the Action header ending in "/Probe".
func isDiscoveryProbe(b []byte) bool {
	action := FindTagValue(b, "Action")
	return strings.HasSuffix(strings.TrimSpace(action), "/Probe")
}

// buildXAddrs returns a space-separated list of ONVIF device service URLs,
// one for each non-loopback IPv4 address on the host.
func buildXAddrs(apiPort int) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	port := strconv.Itoa(apiPort)
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
		xaddrs = append(xaddrs, "http://"+ip4.String()+":"+port+PathDevice)
	}
	return strings.Join(xaddrs, " ")
}

// buildProbeMatchResponse builds a WS-Discovery ProbeMatches SOAP envelope.
// relatesTo is the MessageID from the incoming Probe (may be empty).
func buildProbeMatchResponse(relatesTo, xaddrs string) []byte {
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
          <a:Address>urn:uuid:` + deviceUUID + `</a:Address>
        </a:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes>onvif://www.onvif.org/name/go2rtc onvif://www.onvif.org/location/github onvif://www.onvif.org/Profile/Streaming onvif://www.onvif.org/type/Network_Video_Transmitter</d:Scopes>
        <d:XAddrs>` + xaddrs + `</d:XAddrs>
        <d:MetadataVersion>1</d:MetadataVersion>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`)
}
