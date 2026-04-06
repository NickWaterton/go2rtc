package onvif

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type OnvifProfile struct {
	Name    string   `yaml:"name"`
	Port    int      `yaml:"port"`    // if > 0, this camera gets its own HTTP server on this port
	Streams []string `yaml:"streams"`
}

const ServiceGetServiceCapabilities = "GetServiceCapabilities"

const (
	DeviceGetCapabilities          = "GetCapabilities"
	DeviceGetDeviceInformation     = "GetDeviceInformation"
	DeviceGetDiscoveryMode         = "GetDiscoveryMode"
	DeviceGetDNS                   = "GetDNS"
	DeviceGetHostname              = "GetHostname"
	DeviceGetNetworkDefaultGateway = "GetNetworkDefaultGateway"
	DeviceGetNetworkInterfaces     = "GetNetworkInterfaces"
	DeviceGetNetworkProtocols      = "GetNetworkProtocols"
	DeviceGetNTP                   = "GetNTP"
	DeviceGetOSDs                  = "GetOSDs"
	DeviceGetOSDOptions            = "GetOSDOptions"
	DeviceGetScopes                = "GetScopes"
	DeviceGetServices              = "GetServices"
	DeviceGetSystemDateAndTime     = "GetSystemDateAndTime"
	DeviceSystemReboot             = "SystemReboot"
)

const (
	MediaGetAudioEncoderConfigurations = "GetAudioEncoderConfigurations"
	MediaGetAudioSources               = "GetAudioSources"
	MediaGetAudioSourceConfigurations  = "GetAudioSourceConfigurations"
	MediaGetProfile                    = "GetProfile"
	MediaGetProfiles                   = "GetProfiles"
	MediaGetSnapshotUri                = "GetSnapshotUri"
	MediaGetStreamUri                  = "GetStreamUri"
	MediaGetVideoEncoderConfigurations = "GetVideoEncoderConfigurations"
	MediaGetVideoSources               = "GetVideoSources"
	MediaGetVideoSourceConfiguration   = "GetVideoSourceConfiguration"
	MediaGetVideoSourceConfigurations  = "GetVideoSourceConfigurations"
)

// Package-level compiled regexes (avoids recompilation on every call).
var (
	reRequestAction = regexp.MustCompile(`Body[^<]+<([^ />]+)`)
	reRes           = regexp.MustCompile(`res=(\d+)x(\d+)`)
	reCodec         = regexp.MustCompile(`codec=([a-zA-Z0-9]+)`)
	reFramerate     = regexp.MustCompile(`framerate=(\d+)`)
	reKbps          = regexp.MustCompile(`kbps=(\d+)`)
	reAudio         = regexp.MustCompile(`audio=([a-zA-Z0-9]+)`)
)

func GetRequestAction(b []byte) string {
	// <soap-env:Body><ns0:GetCapabilities xmlns:ns0="http://www.onvif.org/ver10/device/wsdl">
	// <v:Body><GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl" /></v:Body>
	m := reRequestAction.FindSubmatch(b)
	if len(m) != 2 {
		return ""
	}
	if i := bytes.IndexByte(m[1], ':'); i > 0 {
		return string(m[1][i+1:])
	}
	return string(m[1])
}

func GetCapabilitiesResponse(host string) []byte {
	e := NewEnvelope()
	e.Append(`<tds:GetCapabilitiesResponse>
	<tds:Capabilities>
		<tt:Device>
			<tt:XAddr>http://`, host, `/onvif/device_service</tt:XAddr>
		</tt:Device>
		<tt:Media>
			<tt:XAddr>http://`, host, `/onvif/media_service</tt:XAddr>
			<tt:StreamingCapabilities>
				<tt:RTPMulticast>false</tt:RTPMulticast>
				<tt:RTP_TCP>false</tt:RTP_TCP>
				<tt:RTP_RTSP_TCP>true</tt:RTP_RTSP_TCP>
			</tt:StreamingCapabilities>
		</tt:Media>
	</tds:Capabilities>
</tds:GetCapabilitiesResponse>`)
	return e.Bytes()
}

func GetServicesResponse(host string) []byte {
	e := NewEnvelope()
	e.Append(`<tds:GetServicesResponse>
	<tds:Service>
		<tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace>
		<tds:XAddr>http://`, host, `/onvif/device_service</tds:XAddr>
		<tds:Version><tt:Major>2</tt:Major><tt:Minor>5</tt:Minor></tds:Version>
	</tds:Service>
	<tds:Service>
		<tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace>
		<tds:XAddr>http://`, host, `/onvif/media_service</tds:XAddr>
		<tds:Version><tt:Major>2</tt:Major><tt:Minor>5</tt:Minor></tds:Version>
	</tds:Service>
</tds:GetServicesResponse>`)
	return e.Bytes()
}

func GetSystemDateAndTimeResponse() []byte {
	utc := time.Now().UTC()

	e := NewEnvelope()
	e.Appendf(`<tds:GetSystemDateAndTimeResponse>
	<tds:SystemDateAndTime>
		<tt:DateTimeType>Manual</tt:DateTimeType>
		<tt:DaylightSavings>false</tt:DaylightSavings>
		<tt:TimeZone>
			<tt:TZ>UTC</tt:TZ>
		</tt:TimeZone>
		<tt:UTCDateTime>
			<tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>
			<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>
		</tt:UTCDateTime>
		<tt:LocalDateTime>
			<tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>
			<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>
		</tt:LocalDateTime>
	</tds:SystemDateAndTime>
</tds:GetSystemDateAndTimeResponse>`,
		utc.Hour(), utc.Minute(), utc.Second(), utc.Year(), utc.Month(), utc.Day(),
		utc.Hour(), utc.Minute(), utc.Second(), utc.Year(), utc.Month(), utc.Day(),
	)
	return e.Bytes()
}

func GetDeviceInformationResponse(manuf, model, firmware, serial string) []byte {
	e := NewEnvelope()
	e.Append(`<tds:GetDeviceInformationResponse>
	<tds:Manufacturer>`, manuf, `</tds:Manufacturer>
	<tds:Model>`, model, `</tds:Model>
	<tds:FirmwareVersion>`, firmware, `</tds:FirmwareVersion>
	<tds:SerialNumber>`, serial, `</tds:SerialNumber>
	<tds:HardwareId>1.00</tds:HardwareId>
</tds:GetDeviceInformationResponse>`)
	return e.Bytes()
}

func GetMediaServiceCapabilitiesResponse() []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetServiceCapabilitiesResponse>
	<trt:Capabilities SnapshotUri="true" Rotation="false" VideoSourceMode="false" OSD="true" TemporaryOSDText="false" EXICompression="false">
		<trt:StreamingCapabilities RTPMulticast="false" RTP_TCP="false" RTP_RTSP_TCP="true" NonAggregateControl="false" NoRTSPStreaming="false" />
	</trt:Capabilities>
</trt:GetServiceCapabilitiesResponse>`)
	return e.Bytes()
}

func GetProfilesResponse(OnvifProfiles []OnvifProfile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetProfilesResponse>
`)
	for _, cam := range OnvifProfiles {
		appendProfile(e, "Profiles", cam)
	}
	e.Append(`</trt:GetProfilesResponse>`)
	return e.Bytes()
}

func GetProfileResponse(cam OnvifProfile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetProfileResponse>
`)
	appendProfile(e, "Profile", cam)
	e.Append(`</trt:GetProfileResponse>`)
	return e.Bytes()
}

// ParseStream splits a stream config string into name, width, height, codec, framerate, kbps, audio.
// Format: "streamName#res=WxH#codec=X#framerate=N#kbps=N#audio=X"
func ParseStream(stream string) (string, int, int, string, int, int, string) {
	parts := strings.Split(stream, "#")
	name := parts[0]
	width, height := 1920, 1080
	codec := "H264"
	framerate := 30
	kbps := 0
	audio := ""

	for _, part := range parts[1:] {
		if m := reRes.FindStringSubmatch(part); len(m) == 3 {
			width, _ = strconv.Atoi(m[1])
			height, _ = strconv.Atoi(m[2])
		}
		if m := reCodec.FindStringSubmatch(part); len(m) == 2 {
			codec = m[1]
		}
		if m := reFramerate.FindStringSubmatch(part); len(m) == 2 {
			framerate, _ = strconv.Atoi(m[1])
		}
		if m := reKbps.FindStringSubmatch(part); len(m) == 2 {
			kbps, _ = strconv.Atoi(m[1])
		}
		if m := reAudio.FindStringSubmatch(part); len(m) == 2 {
			audio = m[1]
		}
	}

	return name, width, height, codec, framerate, kbps, audio
}

// StreamNameFromConfigToken extracts the bare stream name from a VideoSourceConfiguration token.
// Handles both bare stream names ("camera1") and srccfg tokens ("camera1_srccfg_0").
func StreamNameFromConfigToken(token string) string {
	if idx := strings.LastIndex(token, "_srccfg_"); idx >= 0 {
		return token[:idx]
	}
	return token
}

func appendProfile(e *Envelope, tag string, profile OnvifProfile) {
	if len(profile.Streams) == 0 {
		return
	}

	firstaudiotokenName := ""
	quality := "4"

	for i, stream := range profile.Streams {
		streamName, width, height, codec, framerate, kbps, audio := ParseStream(stream)
		srctokenName := streamName + "_src_" + strconv.Itoa(i)
		srctcfgtokenName := streamName + "_srccfg_" + strconv.Itoa(i)
		enctokenName := streamName + "_enc_" + strconv.Itoa(i)
		audiotokenName := streamName + "_audio_" + strconv.Itoa(i)

		if i == 0 {
			if audio != "" {
				firstaudiotokenName = audiotokenName
			}
		} else {
			quality = "1"
		}

		e.Append(`<trt:`, tag, ` token="`, streamName, `" fixed="true">
    <tt:Name>`, streamName, `</tt:Name>
    <tt:VideoSourceConfiguration token="`, srctcfgtokenName, `">
        <tt:Name>Video`, streamName, `</tt:Name>
        <tt:UseCount>1</tt:UseCount>
        <tt:SourceToken>`, srctokenName, `</tt:SourceToken>
        <tt:Bounds x="0" y="0" width="`, strconv.Itoa(width), `" height="`, strconv.Itoa(height), `"/>
    </tt:VideoSourceConfiguration>
    <tt:VideoEncoderConfiguration token="`, enctokenName, `">
        <tt:Name>Encoder`, streamName, `</tt:Name>
        <tt:UseCount>1</tt:UseCount>
        <tt:Encoding>`, codec, `</tt:Encoding>
        <tt:Resolution><tt:Width>`, strconv.Itoa(width), `</tt:Width><tt:Height>`, strconv.Itoa(height), `</tt:Height></tt:Resolution>
        <tt:Quality>`, quality, `</tt:Quality>
        <tt:RateControl><tt:FrameRateLimit>`, strconv.Itoa(framerate), `</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>`, strconv.Itoa(kbps), `</tt:BitrateLimit></tt:RateControl>
        <tt:H264>
            <tt:GovLength>60</tt:GovLength>
            <tt:H264Profile>Main</tt:H264Profile>
        </tt:H264>
        <tt:Multicast>
            <tt:Address>
                <tt:Type>IPv4</tt:Type>
                <tt:IPv4Address>0.0.0.0</tt:IPv4Address>
            </tt:Address>
            <tt:Port>0</tt:Port>
            <tt:TTL>1</tt:TTL>
            <tt:AutoStart>false</tt:AutoStart>
        </tt:Multicast>
        <tt:SessionTimeout>PT60S</tt:SessionTimeout>
    </tt:VideoEncoderConfiguration>
`)
		if audio != "" {
			e.Append(`    <tt:AudioEncoderConfiguration token="`, audiotokenName, `">
        <tt:Name>Audio`, streamName, `</tt:Name>
        <tt:UseCount>2</tt:UseCount>
        <tt:Encoding>`, audio, `</tt:Encoding>
        <tt:Bitrate>64</tt:Bitrate>
        <tt:SampleRate>16000</tt:SampleRate>
    </tt:AudioEncoderConfiguration>
`)
		} else if firstaudiotokenName != "" {
			e.Append(`    <tt:AudioEncoderConfiguration token="`, firstaudiotokenName, `"/>
`)
		}
		e.Append(`</trt:`, tag, `>
`)
	}
}

// GetVideoSourceConfigurationsResponse returns all VideoSourceConfiguration elements,
// one per stream. Each has only the fields required by the ONVIF spec.
func GetVideoSourceConfigurationsResponse(OnvifProfiles []OnvifProfile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourceConfigurationsResponse>
`)
	for _, profile := range OnvifProfiles {
		for i, stream := range profile.Streams {
			name, width, height, _, _, _, _ := ParseStream(stream)
			srctokenName := name + "_src_" + strconv.Itoa(i)
			srctcfgtokenName := name + "_srccfg_" + strconv.Itoa(i)
			e.Append(`<trt:Configurations token="`, srctcfgtokenName, `">
    <tt:Name>Video`, name, `</tt:Name>
    <tt:UseCount>1</tt:UseCount>
    <tt:SourceToken>`, srctokenName, `</tt:SourceToken>
    <tt:Bounds x="0" y="0" width="`, strconv.Itoa(width), `" height="`, strconv.Itoa(height), `"/>
</trt:Configurations>
`)
		}
	}
	e.Append(`</trt:GetVideoSourceConfigurationsResponse>`)
	return e.Bytes()
}

func GetVideoSourceConfigurationResponse(name string, OnvifProfiles []OnvifProfile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourceConfigurationResponse>
`)
	appendVideoSourceConfiguration(e, "Configuration", name, OnvifProfiles)
	e.Append(`</trt:GetVideoSourceConfigurationResponse>`)
	return e.Bytes()
}

func appendVideoSourceConfiguration(e *Envelope, tag, name string, OnvifProfiles []OnvifProfile) {
	// name may be a bare stream name or a VideoSourceConfiguration token (streamName_srccfg_N)
	streamName := StreamNameFromConfigToken(name)
	for _, profile := range OnvifProfiles {
		for i, stream := range profile.Streams {
			sName, width, height, _, _, _, _ := ParseStream(stream)
			if sName == streamName {
				srctokenName := sName + "_src_" + strconv.Itoa(i)
				srctcfgtokenName := sName + "_srccfg_" + strconv.Itoa(i)
				e.Append(`<trt:`, tag, ` token="`, srctcfgtokenName, `">
    <tt:Name>Video`, sName, `</tt:Name>
    <tt:UseCount>1</tt:UseCount>
    <tt:SourceToken>`, srctokenName, `</tt:SourceToken>
    <tt:Bounds x="0" y="0" width="`, strconv.Itoa(width), `" height="`, strconv.Itoa(height), `"/>
</trt:`, tag, `>
`)
			}
		}
	}
}

func GetVideoSourcesResponse(OnvifProfiles []OnvifProfile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourcesResponse>
`)
	for _, profile := range OnvifProfiles {
		for i, stream := range profile.Streams {
			name, width, height, _, framerate, _, _ := ParseStream(stream)
			srctokenName := name + "_src_" + strconv.Itoa(i)
			e.Append(`<tt:VideoSources token="`, srctokenName, `">
    <tt:Framerate>`, strconv.Itoa(framerate), `</tt:Framerate>
    <tt:Resolution><tt:Width>`, strconv.Itoa(width), `</tt:Width><tt:Height>`, strconv.Itoa(height), `</tt:Height></tt:Resolution>
</tt:VideoSources>
`)
		}
	}
	e.Append(`</trt:GetVideoSourcesResponse>
`)
	return e.Bytes()
}

func GetVideoEncoderConfigurationsResponse(OnvifProfiles []OnvifProfile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoEncoderConfigurationsResponse>
`)
	for _, profile := range OnvifProfiles {
		for i, stream := range profile.Streams {
			name, width, height, codec, framerate, kbps, _ := ParseStream(stream)
			enctokenName := name + "_enc_" + strconv.Itoa(i)
			quality := "4"
			if i > 0 {
				quality = "1"
			}
			e.Append(`<tt:VideoEncoderConfiguration token="`, enctokenName, `">
    <tt:Name>Encoder`, name, `</tt:Name>
    <tt:Encoding>`, codec, `</tt:Encoding>
    <tt:Quality>`, quality, `</tt:Quality>
    <tt:Resolution><tt:Width>`, strconv.Itoa(width), `</tt:Width><tt:Height>`, strconv.Itoa(height), `</tt:Height></tt:Resolution>
    <tt:RateControl><tt:FrameRateLimit>`, strconv.Itoa(framerate), `</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>`, strconv.Itoa(kbps), `</tt:BitrateLimit></tt:RateControl>
    <tt:H264>
        <tt:GovLength>60</tt:GovLength>
        <tt:H264Profile>Main</tt:H264Profile>
    </tt:H264>
    <tt:Multicast>
        <tt:Address>
            <tt:Type>IPv4</tt:Type>
            <tt:IPv4Address>0.0.0.0</tt:IPv4Address>
        </tt:Address>
        <tt:Port>0</tt:Port>
        <tt:TTL>1</tt:TTL>
        <tt:AutoStart>false</tt:AutoStart>
    </tt:Multicast>
    <tt:SessionTimeout>PT60S</tt:SessionTimeout>
</tt:VideoEncoderConfiguration>
`)
		}
	}
	e.Append(`</trt:GetVideoEncoderConfigurationsResponse>`)
	return e.Bytes()
}

func GetStreamUriResponse(uri string) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>`, uri, `</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`)
	return e.Bytes()
}

func GetSnapshotUriResponse(uri string) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>`, uri, `</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse>`)
	return e.Bytes()
}

func GetOSDOptionsResponse() []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetOSDOptionsResponse>
	<trt:OSDOptions>
		<tt:MaximumNumberOfOSDs Total="2" Image="0" PlainText="1" Date="0" Time="0" DateAndTime="1"/>
		<tt:Type>Text</tt:Type>
		<tt:PositionOption>UpperLeft</tt:PositionOption>
		<tt:PositionOption>UpperRight</tt:PositionOption>
		<tt:PositionOption>LowerLeft</tt:PositionOption>
		<tt:PositionOption>LowerRight</tt:PositionOption>
		<tt:PositionOption>Custom</tt:PositionOption>
		<tt:TextOption>
			<tt:Type>Plain</tt:Type>
			<tt:Type>DateAndTime</tt:Type>
		</tt:TextOption>
	</trt:OSDOptions>
</trt:GetOSDOptionsResponse>`)
	return e.Bytes()
}

// GetOSDsResponse returns OSD definitions for the given VideoSourceConfiguration token.
// configurationToken should be a srccfg token (e.g., "camera1_srccfg_0").
func GetOSDsResponse(configurationToken string, cameraName string) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetOSDsResponse>
    <trt:OSDs token="OSD_TimeStamp">
        <tt:Name>TimeStamp</tt:Name>
        <tt:UseCount>2</tt:UseCount>
		<tt:VideoSourceConfigurationToken>`, configurationToken, `</tt:VideoSourceConfigurationToken>
		<tt:Type>Text</tt:Type>
		<tt:Position>
			<tt:Type>Custom</tt:Type>
			<tt:Pos x="0.1" y="0"/>
		</tt:Position>
		<tt:TextString>
			<tt:Type>DateAndTime</tt:Type>
			<tt:DateFormat>yyyy-MM-dd</tt:DateFormat>
			<tt:TimeFormat>HH:mm:ss</tt:TimeFormat>
			<tt:FontSize>20</tt:FontSize>
			<tt:FontColor>#FFFFFFFF</tt:FontColor>
			<tt:BackgroundColor>#40000000</tt:BackgroundColor>
		</tt:TextString>
	</trt:OSDs>
	<trt:OSDs token="OSD_Label">
		<tt:Name>CameraLabel</tt:Name>
		<tt:UseCount>2</tt:UseCount>
		<tt:VideoSourceConfigurationToken>`, configurationToken, `</tt:VideoSourceConfigurationToken>
		<tt:Type>Text</tt:Type>
		<tt:Position>
			<tt:Type>Custom</tt:Type>
			<tt:Pos x="0" y="0"/>
		</tt:Position>
		<tt:TextString>
			<tt:Type>Plain</tt:Type>
			<tt:PlainText>`, cameraName, `</tt:PlainText>
			<tt:FontSize>20</tt:FontSize>
			<tt:FontColor>#FFFFFFFF</tt:FontColor>
			<tt:BackgroundColor>#40000000</tt:BackgroundColor>
		</tt:TextString>
	</trt:OSDs>
</trt:GetOSDsResponse>`)
	return e.Bytes()
}

// GetScopesResponse returns a dynamic GetScopes response using the given device name.
// Spaces in name are percent-encoded so the scope URI is valid.
func GetScopesResponse(name string) []byte {
	encoded := strings.ReplaceAll(name, " ", "%20")
	e := NewEnvelope()
	e.Append(`<tds:GetScopesResponse>
	<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/name/`, encoded, `</tt:ScopeItem></tds:Scopes>
	<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/location/github</tt:ScopeItem></tds:Scopes>
	<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/Profile/Streaming</tt:ScopeItem></tds:Scopes>
	<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/type/Network_Video_Transmitter</tt:ScopeItem></tds:Scopes>
</tds:GetScopesResponse>`)
	return e.Bytes()
}

func StaticResponse(operation string) []byte {
	switch operation {
	case DeviceGetSystemDateAndTime:
		return GetSystemDateAndTimeResponse()
	}

	e := NewEnvelope()
	e.Append(responses[operation])
	return e.Bytes()
}

var responses = map[string]string{
	DeviceGetDiscoveryMode:         `<tds:GetDiscoveryModeResponse><tds:DiscoveryMode>Discoverable</tds:DiscoveryMode></tds:GetDiscoveryModeResponse>`,
	DeviceGetDNS:                   `<tds:GetDNSResponse><tds:DNSInformation /></tds:GetDNSResponse>`,
	DeviceGetHostname:              `<tds:GetHostnameResponse><tds:HostnameInformation /></tds:GetHostnameResponse>`,
	DeviceGetNetworkDefaultGateway: `<tds:GetNetworkDefaultGatewayResponse><tds:NetworkGateway /></tds:GetNetworkDefaultGatewayResponse>`,
	DeviceGetNTP:                   `<tds:GetNTPResponse><tds:NTPInformation /></tds:GetNTPResponse>`,
	DeviceSystemReboot:             `<tds:SystemRebootResponse><tds:Message>OK</tds:Message></tds:SystemRebootResponse>`,

	DeviceGetNetworkInterfaces: `<tds:GetNetworkInterfacesResponse />`,
	DeviceGetNetworkProtocols:  `<tds:GetNetworkProtocolsResponse />`,

	MediaGetAudioEncoderConfigurations: `<trt:GetAudioEncoderConfigurationsResponse />`,
	MediaGetAudioSources:               `<trt:GetAudioSourcesResponse />`,
	MediaGetAudioSourceConfigurations:  `<trt:GetAudioSourceConfigurationsResponse />`,
}
