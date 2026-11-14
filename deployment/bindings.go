package deployment

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// BindingsEnvironment selects the root-owned descriptor for this role invocation.
const BindingsEnvironment = "FOUNDRY_DEPLOY_RUNTIME_FILE"
const maxBindingsBytes = 4 << 20

// BindingName identifies a binding declared by the consuming role.
type BindingName string

// RoleName identifies a builtin or executable role in the topology.
type RoleName string

// ListenerName identifies a listener within its producing role.
type ListenerName string

// InstanceID is the UUID of the deployed environment instance.
type InstanceID string

// SHA256 is a lowercase hexadecimal SHA-256 digest.
type SHA256 string

// EndpointScheme identifies the protocol used to address a private endpoint.
type EndpointScheme string

// ListenerTransport identifies a declared listener's network transport.
type ListenerTransport string

// TCPTransport carries a public listener over TCP.
const TCPTransport ListenerTransport = "tcp"

// TLSMode identifies where a public listener terminates TLS.
type TLSMode string

const (
	// TLSEdge terminates TLS at the deployment's public edge.
	TLSEdge TLSMode = "edge"
	// TLSNone exposes the listener without TLS termination.
	TLSNone TLSMode = "none"
)

const (
	// HTTP addresses a private HTTP listener.
	HTTP EndpointScheme = "http"
	// TCP addresses a private TCP listener.
	TCP EndpointScheme = "tcp"
)

// Unit identifies a single placed service. Ordinals above one require a later capability.
type Unit struct {
	// Instance identifies the deployed environment instance.
	Instance InstanceID
	// Role identifies the placed service role.
	Role RoleName
	// Ordinal is the service copy number; the preview supports only one.
	Ordinal uint16
}

// Executable is a host-verified absolute file, never a PATH search or shell command.
type Executable struct {
	// Path is the absolute path of the host-verified executable.
	Path string
	// SHA256 identifies the executable source recorded by the host.
	SHA256 SHA256
}

// Endpoint is an allocated private endpoint. It is immutable for this invocation.
type Endpoint struct {
	// Role identifies the producer of this endpoint.
	Role RoleName
	// Listener identifies the producer's listener.
	Listener ListenerName
	// Scheme selects HTTP or TCP addressing.
	Scheme EndpointScheme
	// Address is the allocated loopback address.
	Address netip.Addr
	// Port is the allocated private port, not the public edge port.
	Port uint16
}

// URL returns the endpoint scheme and host with its port, without a path.
func (e Endpoint) URL() url.URL {
	return url.URL{Scheme: string(e.Scheme), Host: net.JoinHostPort(e.Address.String(), strconv.Itoa(int(e.Port)))}
}

// PublicListener is the declared public face of a placed listener.
// It is immutable for this invocation; it does not attest to current readiness.
type PublicListener struct {
	// Role identifies the role that owns the listener.
	Role RoleName
	// Listener identifies the declared listener within its role.
	Listener ListenerName
	// Hostname is the public DNS hostname selected by the profile.
	Hostname string
	// Port is the public port selected by the profile.
	Port uint16
	// TLS is the TLS mode declared by the topology.
	TLS TLSMode
	// Transport is the network transport declared by the topology.
	Transport ListenerTransport
	// PublicAddresses contains the profile's explicit public addresses, not a DNS lookup.
	PublicAddresses []netip.Addr
}

// BundleDirectory is the protected service-local view, not the artifact's host path.
type BundleDirectory struct {
	// Path is the absolute path of the protected directory view.
	Path string
	// SHA256 identifies the artifact that supplies this bundle.
	SHA256 SHA256
}

// Bindings is an immutable startup snapshot. Pass concrete values to business handlers.
// Its zero value has no bindings; a missing name returns fault.Missing.
type Bindings struct {
	unit            Unit
	deployment      SHA256
	executables     map[BindingName]Executable
	endpoints       map[BindingName]Endpoint
	bundles         map[BindingName]BundleDirectory
	publicListeners map[BindingName]PublicListener
}

// Unit returns the identity of the role invocation that owns these bindings.
func (b Bindings) Unit() Unit { return b.unit }

// DeploymentSHA256 returns the digest of the selected deployment.
func (b Bindings) DeploymentSHA256() SHA256 { return b.deployment }

// Executable returns the named executable.
// A missing name returns an error classified as fault.Missing.
func (b Bindings) Executable(name BindingName) (Executable, error) {
	value, ok := b.executables[name]
	if !ok {
		return Executable{}, safeerror.NewFault(fault.Missing, "deployment executable binding is missing")
	}
	return value, nil
}

// Endpoint returns the named private endpoint.
// A missing name returns an error classified as fault.Missing.
func (b Bindings) Endpoint(name BindingName) (Endpoint, error) {
	value, ok := b.endpoints[name]
	if !ok {
		return Endpoint{}, safeerror.NewFault(fault.Missing, "deployment endpoint binding is missing")
	}
	return value, nil
}

// PublicListener returns the named public listener with an owned address snapshot.
// A missing name returns an error classified as fault.Missing.
func (b Bindings) PublicListener(name BindingName) (PublicListener, error) {
	value, ok := b.publicListeners[name]
	if !ok {
		return PublicListener{}, safeerror.NewFault(fault.Missing, "deployment public listener binding is missing")
	}
	value.PublicAddresses = slices.Clone(value.PublicAddresses)
	return value, nil
}

// Bundle returns the named service-local bundle directory.
// A missing name returns an error classified as fault.Missing.
func (b Bindings) Bundle(name BindingName) (BundleDirectory, error) {
	value, ok := b.bundles[name]
	if !ok {
		return BundleDirectory{}, safeerror.NewFault(fault.Missing, "deployment bundle binding is missing")
	}
	return value, nil
}

// LoadBindings reads the descriptor selected by the deployment unit. Absence is
// reported separately from malformed configuration; an explicit bad path never falls back.
func LoadBindings() (Bindings, bool, error) {
	path, exists := os.LookupEnv(BindingsEnvironment)
	if !exists {
		return Bindings{}, false, nil
	}
	value, err := ReadBindings(path)
	return value, err == nil, err
}

// ReadBindings loads a bounded, nonsecret descriptor. Host-side executable and
// artifact verification remains the deployment owner's responsibility.
func ReadBindings(path string) (Bindings, error) {
	wire, err := readFeatureDescriptor(path)
	if err != nil {
		return Bindings{}, err
	}
	return wire.bindings()
}

type unitWire struct {
	Instance string `json:"instance_id"`
	Role     string `json:"role"`
	Ordinal  int    `json:"ordinal"`
}
type executableWire struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"source_sha256"`
}
type endpointWire struct {
	Name     string `json:"name"`
	Endpoint struct {
		Role     string `json:"role"`
		Listener string `json:"listener"`
		Address  string `json:"address"`
		Port     int    `json:"port"`
	} `json:"endpoint"`
	Scheme string `json:"scheme"`
}
type bundleWire struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"artifact_sha256"`
}
type publicListenerWire struct {
	Name            string            `json:"name"`
	Role            string            `json:"role"`
	Listener        string            `json:"listener"`
	Hostname        string            `json:"hostname"`
	Port            int               `json:"port"`
	TLS             TLSMode           `json:"tls"`
	Transport       ListenerTransport `json:"transport"`
	PublicAddresses []string          `json:"public_addresses"`
}
type featureDescriptor struct {
	Schema     int      `json:"schema"`
	Instance   string   `json:"instance"`
	User       string   `json:"user"`
	EnvFile    string   `json:"env_file"`
	WorkDir    string   `json:"work_dir"`
	Release    string   `json:"release"`
	Deployment string   `json:"deployment_sha256"`
	Unit       unitWire `json:"unit"`
	Bindings   struct {
		Schema          int                  `json:"schema"`
		Role            string               `json:"role"`
		Executables     []executableWire     `json:"executables"`
		Endpoints       []endpointWire       `json:"endpoints"`
		Bundles         []bundleWire         `json:"bundles"`
		PublicListeners []publicListenerWire `json:"public_listeners,omitempty"`
	} `json:"bindings"`
}

var bindingIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var digestIdentifier = regexp.MustCompile(`^[a-f0-9]{64}$`)
var instanceIdentifier = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
var hostnameLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func publicHostname(hostname string) bool {
	labels := strings.Split(hostname, ".")
	if len(hostname) > 253 || len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !hostnameLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func absolutePath(path string) bool {
	return len(path) <= 1024 && filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/" &&
		!strings.Contains(path, "\\") && strings.IndexFunc(path, func(r rune) bool { return r < 32 || r == 127 }) < 0
}

func invalidBindings() error {
	return safeerror.NewFault(fault.Invalid, "invalid deployment binding descriptor")
}

func readFeatureDescriptor(path string) (featureDescriptor, error) {
	var wire featureDescriptor
	if !absolutePath(path) {
		return wire, invalidBindings()
	}
	file, err := os.Open(path)
	if err != nil {
		return wire, safeerror.WrapFault(fault.Internal, "cannot open deployment binding descriptor", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return wire, invalidBindings()
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBindingsBytes+1))
	if err != nil {
		return wire, safeerror.WrapFault(fault.Internal, "cannot read deployment binding descriptor", err)
	}
	canonical, err := jsonwire.Canonical(data, jsonwire.Limits{Bytes: maxBindingsBytes, Depth: 12, Nodes: 8192})
	if err != nil {
		return wire, invalidBindings()
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&wire) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		return featureDescriptor{}, invalidBindings()
	}
	if wire.Schema != 2 || !instanceName.MatchString(wire.Instance) || wire.User != wire.Instance || wire.User == "root" ||
		!absolutePath(wire.EnvFile) || !absolutePath(wire.WorkDir) || !absolutePath(wire.Release) {
		return featureDescriptor{}, invalidBindings()
	}
	if _, err := wire.bindings(); err != nil {
		return featureDescriptor{}, err
	}
	return wire, nil
}

func (w featureDescriptor) bindings() (Bindings, error) {
	if !digestIdentifier.MatchString(w.Deployment) || !instanceIdentifier.MatchString(w.Unit.Instance) ||
		!bindingIdentifier.MatchString(w.Unit.Role) || w.Unit.Ordinal != 1 || w.Bindings.Schema != 1 ||
		w.Bindings.Role != w.Unit.Role || len(w.Bindings.Executables) > 128 || len(w.Bindings.Endpoints) > 128 || len(w.Bindings.Bundles) > 64 ||
		w.Bindings.Executables == nil || w.Bindings.Endpoints == nil || w.Bindings.Bundles == nil || len(w.Bindings.PublicListeners) > 128 {
		return Bindings{}, invalidBindings()
	}
	b := Bindings{unit: Unit{InstanceID(w.Unit.Instance), RoleName(w.Unit.Role), 1}, deployment: SHA256(w.Deployment),
		executables: make(map[BindingName]Executable), endpoints: make(map[BindingName]Endpoint), bundles: make(map[BindingName]BundleDirectory),
		publicListeners: make(map[BindingName]PublicListener)}
	names := make(map[string]bool)
	unique := func(name string) bool {
		if !bindingIdentifier.MatchString(name) || names[name] {
			return false
		}
		names[name] = true
		return true
	}
	for _, v := range w.Bindings.Executables {
		if !unique(v.Name) || !absolutePath(v.Path) || !digestIdentifier.MatchString(v.SHA256) {
			return Bindings{}, invalidBindings()
		}
		b.executables[BindingName(v.Name)] = Executable{v.Path, SHA256(v.SHA256)}
	}
	for _, v := range w.Bindings.Endpoints {
		address, err := netip.ParseAddr(v.Endpoint.Address)
		if !unique(v.Name) || err != nil || (address.String() != "127.0.0.1" && address.String() != "::1") ||
			!bindingIdentifier.MatchString(v.Endpoint.Role) || !bindingIdentifier.MatchString(v.Endpoint.Listener) ||
			v.Endpoint.Port < 1024 || v.Endpoint.Port > 65535 || (v.Scheme != string(HTTP) && v.Scheme != string(TCP)) {
			return Bindings{}, invalidBindings()
		}
		b.endpoints[BindingName(v.Name)] = Endpoint{RoleName(v.Endpoint.Role), ListenerName(v.Endpoint.Listener), EndpointScheme(v.Scheme), address, uint16(v.Endpoint.Port)}
	}
	for _, v := range w.Bindings.Bundles {
		if !unique(v.Name) || !absolutePath(v.Path) || !digestIdentifier.MatchString(v.SHA256) {
			return Bindings{}, invalidBindings()
		}
		b.bundles[BindingName(v.Name)] = BundleDirectory{v.Path, SHA256(v.SHA256)}
	}
	for _, v := range w.Bindings.PublicListeners {
		if !unique(v.Name) || !bindingIdentifier.MatchString(v.Role) || !bindingIdentifier.MatchString(v.Listener) ||
			!publicHostname(v.Hostname) || v.Port < 1 || v.Port > 65535 || (v.TLS != TLSEdge && v.TLS != TLSNone) ||
			v.Transport != TCPTransport || len(v.PublicAddresses) < 1 || len(v.PublicAddresses) > 16 {
			return Bindings{}, invalidBindings()
		}
		addresses := make([]netip.Addr, 0, len(v.PublicAddresses))
		for _, raw := range v.PublicAddresses {
			address, err := netip.ParseAddr(raw)
			if err != nil || address.String() != raw || address.Zone() != "" || address.Is4In6() ||
				address.IsUnspecified() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || slices.Contains(addresses, address) {
				return Bindings{}, invalidBindings()
			}
			addresses = append(addresses, address)
		}
		b.publicListeners[BindingName(v.Name)] = PublicListener{Role: RoleName(v.Role), Listener: ListenerName(v.Listener),
			Hostname: v.Hostname, Port: uint16(v.Port), TLS: v.TLS, Transport: v.Transport, PublicAddresses: addresses}
	}
	return b, nil
}
