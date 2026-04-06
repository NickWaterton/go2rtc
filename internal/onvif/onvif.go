package onvif

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/AlexxIT/go2rtc/internal/api"
	"github.com/AlexxIT/go2rtc/internal/app"
	"github.com/AlexxIT/go2rtc/internal/rtsp"
	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
	"github.com/rs/zerolog"
)

var OnvifProfiles []onvif.OnvifProfile

func Init() {
	var cfg struct {
		Onvif struct {
			OnvifProfiles []onvif.OnvifProfile `yaml:"profiles"`
		} `yaml:"onvif"`
	}

	app.LoadConfig(&cfg)
	OnvifProfiles = cfg.Onvif.OnvifProfiles

	log = app.GetLogger("onvif")

	streams.HandleFunc("onvif", streamOnvif)

	// Main ONVIF server on the go2rtc API port — serves all profiles.
	api.HandleFunc("/onvif/", makeOnvifHandler(OnvifProfiles, api.Port, "go2rtc"))

	// ONVIF client autodiscovery endpoint
	api.HandleFunc("api/onvif", apiOnvif)

	// Per-camera ONVIF servers: each profile with port > 0 gets its own
	// HTTP server that acts as an independent ONVIF device. Register them
	// with the WS-Discovery system before starting the discovery server.
	for _, profile := range OnvifProfiles {
		if profile.Port <= 0 {
			continue
		}
		p := profile // capture loop variable
		var ip net.IP
		if p.IP != "" {
			ip = net.ParseIP(p.IP)
			if ip != nil {
				ip = ip.To4()
			}
			if ip == nil {
				log.Warn().Msgf("[onvif] invalid ip %q for profile %s, using all interfaces", p.IP, p.Name)
			}
		}
		uuid := onvif.RegisterDevice(p.Port, p.Name, ip)
		handler := makeOnvifHandler([]onvif.OnvifProfile{p}, api.Port, p.Name)
		go startCameraServer(ip, p.Port, uuid, handler)
	}

	// Include the main go2rtc device in WS-Discovery only when at least one
	// profile has no dedicated port — otherwise Unifi Protect would also
	// discover the generic "go2rtc" device and display its name pre-adoption.
	includeMain := len(OnvifProfiles) == 0
	for _, p := range OnvifProfiles {
		if p.Port <= 0 {
			includeMain = true
			break
		}
	}

	// WS-Discovery server (must start after all RegisterDevice calls).
	if err := onvif.StartDiscoveryServer(api.Port, "go2rtc", includeMain); err != nil {
		log.Warn().Err(err).Msg("[onvif] WS-Discovery server failed to start (port 3702 in use?)")
	} else {
		log.Info().Int("port", 3702).Msg("[onvif] WS-Discovery server listening")
	}
}

var log zerolog.Logger

// startCameraServer starts a standalone HTTP server for a single ONVIF camera profile.
// ip may be nil to listen on all interfaces, or a specific IP for virtual-IP setups.
func startCameraServer(ip net.IP, port int, uuid string, handler http.HandlerFunc) {
	var addr string
	if ip != nil {
		addr = ip.String() + ":" + strconv.Itoa(port)
	} else {
		addr = ":" + strconv.Itoa(port)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error().Err(err).Msgf("[onvif] camera server failed to start on %s", addr)
		return
	}
	log.Info().Msgf("[onvif] camera server listening on %s (uuid=%s)", addr, uuid)

	mux := http.NewServeMux()
	mux.HandleFunc("/onvif/", handler)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.Serve(ln); err != nil {
		log.Error().Err(err).Msgf("[onvif] camera server error on port %d", port)
	}
}

// makeOnvifHandler returns an ONVIF device service handler scoped to the given
// profiles. mainAPIPort is the go2rtc API port used for snapshot URLs (the
// snapshot endpoint always lives on the main server, not per-camera servers).
// deviceName is advertised in GetScopes and GetDeviceInformation responses.
func makeOnvifHandler(profiles []onvif.OnvifProfile, mainAPIPort int, deviceName string) http.HandlerFunc {
	// Local camera-name lookup scoped to this handler's profiles.
	cameraName := func(streamName string) string {
		for _, p := range profiles {
			for _, s := range p.Streams {
				name, _, _, _, _, _, _ := onvif.ParseStream(s)
				if name == streamName {
					return p.Name
				}
			}
		}
		return "Unknown Camera"
	}

	return func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		operation := onvif.GetRequestAction(b)
		if operation == "" {
			http.Error(w, "malformed request body", http.StatusBadRequest)
			return
		}

		log.Trace().Msgf("[onvif] server request %s %s:\n%s", r.Method, r.RequestURI, b)

		switch operation {
		case onvif.DeviceGetNetworkInterfaces, // important for Hass
			onvif.DeviceGetSystemDateAndTime, // important for Hass
			onvif.DeviceGetDiscoveryMode,
			onvif.DeviceGetDNS,
			onvif.DeviceGetHostname,
			onvif.DeviceGetNetworkDefaultGateway,
			onvif.DeviceGetNetworkProtocols,
			onvif.DeviceGetNTP,
			onvif.MediaGetAudioEncoderConfigurations,
			onvif.MediaGetAudioSources,
			onvif.MediaGetAudioSourceConfigurations:
			b = onvif.StaticResponse(operation)

		case onvif.DeviceGetScopes:
			b = onvif.GetScopesResponse(deviceName)

		case onvif.MediaGetVideoEncoderConfigurations:
			b = onvif.GetVideoEncoderConfigurationsResponse(profiles)

		case onvif.DeviceGetCapabilities:
			// important for Hass: Media section
			b = onvif.GetCapabilitiesResponse(r.Host)

		case onvif.DeviceGetServices:
			b = onvif.GetServicesResponse(r.Host)

		case onvif.DeviceGetOSDs:
			token := onvif.FindTagValue(b, "ConfigurationToken")
			b = onvif.GetOSDsResponse(token, cameraName(onvif.StreamNameFromConfigToken(token)))

		case onvif.DeviceGetOSDOptions:
			b = onvif.GetOSDOptionsResponse()

		case onvif.DeviceGetDeviceInformation:
			// important for Hass: SerialNumber (unique server ID)
			// r.Host includes port so each per-camera server has a unique serial.
			// Unifi Protect auto-names devices as Manufacturer + " " + Model.
			// UP's "Model" field displays the ONVIF Model value.
			// Manufacturer = camera name, Model = "go2rtc" gives:
			//   UP auto-name: "<cameraName> go2rtc" (editable in UP UI)
			//   UP model field: "go2rtc"
			b = onvif.GetDeviceInformationResponse(deviceName, "go2rtc", app.Version, r.Host)

		case onvif.ServiceGetServiceCapabilities:
			// important for Hass
			b = onvif.GetMediaServiceCapabilitiesResponse()

		case onvif.DeviceSystemReboot:
			b = onvif.StaticResponse(operation)

			time.AfterFunc(time.Second, func() {
				os.Exit(0)
			})

		case onvif.MediaGetVideoSources:
			b = onvif.GetVideoSourcesResponse(profiles)

		case onvif.MediaGetProfiles:
			// important for Hass: H264 codec, width, height
			b = onvif.GetProfilesResponse(profiles)

		case onvif.MediaGetProfile:
			token := onvif.FindTagValue(b, "ProfileToken")
			for _, profile := range profiles {
				for _, stream := range profile.Streams {
					name, _, _, _, _, _, _ := onvif.ParseStream(stream)
					if name == token {
						b = onvif.GetProfileResponse(profile)
						break
					}
				}
			}

		case onvif.MediaGetVideoSourceConfigurations:
			// important for Happytime Onvif Client
			b = onvif.GetVideoSourceConfigurationsResponse(profiles)

		case onvif.MediaGetVideoSourceConfiguration:
			token := onvif.FindTagValue(b, "ConfigurationToken")
			b = onvif.GetVideoSourceConfigurationResponse(token, profiles)

		case onvif.MediaGetStreamUri:
			host, _, err := net.SplitHostPort(r.Host)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			uri := "rtsp://" + host + ":" + rtsp.Port + "/" + onvif.FindTagValue(b, "ProfileToken")
			log.Debug().Msgf("[onvif] MediaGetStreamUri URL: %s", uri)
			b = onvif.GetStreamUriResponse(uri)

		case onvif.MediaGetSnapshotUri:
			// Snapshot is always served by the main go2rtc API, not the per-camera
			// server, so use mainAPIPort rather than whatever port this request arrived on.
			host, _, err := net.SplitHostPort(r.Host)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			uri := "http://" + host + ":" + strconv.Itoa(mainAPIPort) + "/api/frame.jpeg?src=" + onvif.FindTagValue(b, "ProfileToken")
			log.Debug().Msgf("[onvif] Snapshot URL: %s", uri)
			b = onvif.GetSnapshotUriResponse(uri)

		default:
			http.Error(w, "unsupported operation", http.StatusBadRequest)
			log.Warn().Msgf("[onvif] unsupported operation: %s", operation)
			log.Debug().Msgf("[onvif] unsupported request:\n%s", b)
			return
		}

		log.Trace().Msgf("[onvif] server response:\n%s", b)

		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		if _, err = w.Write(b); err != nil {
			log.Error().Err(err).Caller().Send()
		}
	}
}

// GetConfiguredStreams returns the stream names that should be accessible via ONVIF.
func GetConfiguredStreams() []string {
	if len(OnvifProfiles) == 0 {
		return streams.GetAllNames()
	}

	var names []string
	for _, profile := range OnvifProfiles {
		for _, stream := range profile.Streams {
			name, _, _, _, _, _, _ := onvif.ParseStream(stream)
			names = append(names, name)
		}
	}
	return names
}

func streamOnvif(rawURL string) (core.Producer, error) {
	client, err := onvif.NewClient(rawURL)
	if err != nil {
		return nil, err
	}

	uri, err := client.GetURI()
	if err != nil {
		return nil, err
	}

	log.Debug().Msgf("[onvif] new uri=%s", uri)

	return streams.GetProducer(uri)
}

func apiOnvif(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("src")

	var items []*api.Source

	if src == "" {
		urls, err := onvif.DiscoveryStreamingURLs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, rawURL := range urls {
			u, err := url.Parse(rawURL)
			if err != nil {
				log.Warn().Str("url", rawURL).Msg("[onvif] broken")
				continue
			}

			if u.Scheme != "http" {
				log.Warn().Str("url", rawURL).Msg("[onvif] unsupported")
				continue
			}

			u.Scheme = "onvif"
			u.User = url.UserPassword("user", "pass")

			if u.Path == onvif.PathDevice {
				u.Path = ""
			}

			items = append(items, &api.Source{Name: u.Host, URL: u.String()})
		}
	} else {
		client, err := onvif.NewClient(src)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if l := log.Trace(); l.Enabled() {
			b, _ := client.MediaRequest(onvif.MediaGetProfiles)
			l.Msgf("[onvif] src=%s profiles:\n%s", src, b)
		}

		name, err := client.GetName()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tokens, err := client.GetProfilesTokens()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for i, token := range tokens {
			items = append(items, &api.Source{
				Name: name + " stream" + strconv.Itoa(i),
				URL:  src + "?subtype=" + token,
			})
		}

		if len(tokens) > 0 && client.HasSnapshots() {
			items = append(items, &api.Source{
				Name: name + " snapshot",
				URL:  src + "?subtype=" + tokens[0] + "&snapshot",
			})
		}
	}

	api.ResponseSources(w, items)
}
