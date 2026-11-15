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
	"github.com/weiloon1234/Foundry-Go/storage"
)

// BindingsEnvironment selects the root-owned descriptor for this role invocation.
const BindingsEnvironment = "FOUNDRY_DEPLOY_RUNTIME_FILE"
const maxBindingsBytes = 4 << 20
const maxRoleCopies = 128

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
	// Unix addresses a private Unix-domain stream socket.
	Unix EndpointScheme = "unix"
)

// Unit identifies a single placed service copy.
type Unit struct {
	// Instance identifies the deployed environment instance.
	Instance InstanceID
	// Role identifies the placed service role.
	Role RoleName
	// Ordinal is the service copy number, from 1 through 128.
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
	// Scheme selects HTTP, TCP or Unix addressing.
	Scheme EndpointScheme
	// Address is the allocated loopback address.
	Address netip.Addr
	// Port is the allocated private port, not the public edge port.
	Port uint16
	// Path is the full Unix socket path; it is empty for TCP and HTTP endpoints.
	Path string
	// Directory is the shared instance socket directory for Unix endpoints.
	Directory string
	// Name is the Unix socket file name within Directory.
	Name string
}

// Network returns the network accepted by net.Dialer: "unix" or "tcp".
func (e Endpoint) Network() string {
	if e.Scheme == Unix {
		return "unix"
	}
	return "tcp"
}

// URL returns the endpoint URL, including the socket path for Unix endpoints.
func (e Endpoint) URL() url.URL {
	if e.Scheme == Unix {
		return url.URL{Scheme: string(Unix), Path: e.Path}
	}
	return url.URL{Scheme: string(e.Scheme), Host: net.JoinHostPort(e.Address.String(), strconv.Itoa(int(e.Port)))}
}

// Copy is one placed provider and its optional secret file path.
// It contains file paths, never secret values.
type Copy struct {
	// Ordinal is the provider's stable copy number, starting at one.
	Ordinal int
	// Endpoint addresses this copy's declared private listener.
	Endpoint Endpoint
	// SecretFile is empty when no secret file path is included in this descriptor.
	SecretFile string
}

// PublicPort maps a provider copy to its explicit public edge port.
type PublicPort struct {
	// Ordinal is the provider's copy number, starting at one.
	Ordinal int
	// Port is the public port selected by the profile.
	Port uint16
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
	// Port is the first copy's public port, retained for singleton consumers.
	Port uint16
	// Ports contains every placed copy's public port, ordered by ordinal.
	Ports []PublicPort
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

// DirectoryAccess is the filesystem access granted to this role by its sandbox.
type DirectoryAccess string

const (
	// DirectoryReadOnly permits reading prepared files, but no filesystem writes.
	DirectoryReadOnly DirectoryAccess = "read-only"
	// DirectoryReadWrite permits creating, modifying and deleting cached files.
	DirectoryReadWrite DirectoryAccess = "read-write"
)

// SharedDirectory is a disposable, size-bounded tmpfs shared by declared roles.
// Contents are never backed up or restored and must be safe to recreate.
type SharedDirectory struct {
	// Path is the absolute directory path within the role's filesystem view.
	Path string
	// Access is enforced by the systemd sandbox, not by the shared account's mode bits.
	Access DirectoryAccess
	// MaxBytes limits allocated file data. It does not reserve memory or bound sparse file lengths.
	MaxBytes int64
}

// Bindings is an immutable startup snapshot. Pass concrete values to business handlers.
// Its zero value has no bindings; a missing name returns fault.Missing.
type Bindings struct {
	rehearsal         storage.Rehearsal
	unit              Unit
	deployment        SHA256
	executables       map[BindingName]Executable
	endpoints         map[BindingName]Endpoint
	bundles           map[BindingName]BundleDirectory
	publicListeners   map[BindingName]PublicListener
	sharedDirectories map[BindingName]SharedDirectory
	copies            map[BindingName][]Copy
}

// Copies returns the placed providers in ordinal order as an owned slice.
// A declared binding may be empty. An unknown or ungranted name returns fault.Missing.
// Mutating the returned slice or its values cannot change this startup snapshot.
func (b Bindings) Copies(name BindingName) ([]Copy, error) {
	value, ok := b.copies[name]
	if !ok {
		return nil, safeerror.NewFault(fault.Missing, "deployment copies binding is missing")
	}
	return slices.Clone(value), nil
}

// SharedDirectory returns a topology directory explicitly granted to this role.
// A missing or ungranted name returns an error classified as fault.Missing.
func (b Bindings) SharedDirectory(name BindingName) (SharedDirectory, error) {
	value, ok := b.sharedDirectories[name]
	if !ok {
		return SharedDirectory{}, safeerror.NewFault(fault.Missing, "deployment shared directory binding is missing")
	}
	return value, nil
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
	value.Ports = slices.Clone(value.Ports)
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
	return readBindingsOwned(path, rootOwnedDescriptor)
}

func readBindingsOwned(path string, owned func(os.FileInfo) bool) (Bindings, error) {
	wire, err := readFeatureDescriptorOwned(path, owned)
	if err != nil {
		return Bindings{}, err
	}
	return wire.bindings(os.Getenv(RehearsalRunEnvironment))
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
type endpointAddressWire struct {
	Role      string  `json:"role"`
	Ordinal   *int    `json:"ordinal,omitempty"`
	Listener  string  `json:"listener"`
	Address   *string `json:"address"`
	Port      *int    `json:"port"`
	Directory *string `json:"directory"`
	Name      *string `json:"name"`
	Path      *string `json:"path"`
}
type endpointWire struct {
	Name     string              `json:"name"`
	Endpoint endpointAddressWire `json:"endpoint"`
	Scheme   string              `json:"scheme"`
}
type copyWire struct {
	Ordinal    int                 `json:"ordinal"`
	Endpoint   endpointAddressWire `json:"endpoint"`
	Scheme     string              `json:"scheme"`
	SecretFile json.RawMessage     `json:"secret_file,omitempty"`
}
type copiesWire struct {
	Name   string     `json:"name"`
	Copies []copyWire `json:"copies"`
}
type publicPortWire struct {
	Ordinal int `json:"ordinal"`
	Port    int `json:"port"`
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
	Ports           json.RawMessage   `json:"ports,omitempty"`
}
type sharedDirectoryWire struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	Access   DirectoryAccess `json:"access"`
	MaxBytes int64           `json:"max_bytes"`
}
type featureDescriptor struct {
	Rehearsal  *rehearsalWire `json:"rehearsal,omitempty"`
	Schema     int            `json:"schema"`
	Instance   string         `json:"instance"`
	User       string         `json:"user"`
	EnvFile    string         `json:"env_file"`
	WorkDir    string         `json:"work_dir"`
	Release    string         `json:"release"`
	Deployment string         `json:"deployment_sha256"`
	Unit       unitWire       `json:"unit"`
	Bindings   struct {
		Schema            int                   `json:"schema"`
		Role              string                `json:"role"`
		Executables       []executableWire      `json:"executables"`
		Endpoints         []endpointWire        `json:"endpoints"`
		Bundles           []bundleWire          `json:"bundles"`
		PublicListeners   []publicListenerWire  `json:"public_listeners,omitempty"`
		SharedDirectories []sharedDirectoryWire `json:"shared_directories,omitempty"`
		Copies            []copiesWire          `json:"copies,omitempty"`
	} `json:"bindings"`
}

var socketIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)
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
	return readFeatureDescriptorOwned(path, rootOwnedDescriptor)
}

func readFeatureDescriptorOwned(path string, owned func(os.FileInfo) bool) (featureDescriptor, error) {
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
	canonical, err := jsonwire.Canonical(data, jsonwire.Limits{Bytes: maxBindingsBytes, Depth: 12, Nodes: 1 << 19})
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
	if wire.Rehearsal != nil && !owned(info) {
		return featureDescriptor{}, invalidBindings()
	}
	if _, err := wire.bindings(""); err != nil {
		return featureDescriptor{}, err
	}
	return wire, nil
}

func copySocketName(name string, ordinal *int) bool {
	if ordinal != nil && *ordinal > 1 {
		suffix := "@" + strconv.Itoa(*ordinal)
		if !strings.HasSuffix(name, suffix) {
			return false
		}
		name = strings.TrimSuffix(name, suffix)
	}
	return socketIdentifier.MatchString(name)
}

func resolveEndpoint(v endpointWire, instance string) (Endpoint, error) {
	if !bindingIdentifier.MatchString(v.Endpoint.Role) || !bindingIdentifier.MatchString(v.Endpoint.Listener) ||
		(v.Endpoint.Ordinal != nil && (*v.Endpoint.Ordinal < 1 || *v.Endpoint.Ordinal > maxRoleCopies)) {
		return Endpoint{}, invalidBindings()
	}
	endpoint := Endpoint{Role: RoleName(v.Endpoint.Role), Listener: ListenerName(v.Endpoint.Listener), Scheme: EndpointScheme(v.Scheme)}
	if endpoint.Scheme == Unix {
		directory := "/run/foundry/services/" + instance + "/sockets"
		if v.Endpoint.Address != nil || v.Endpoint.Port != nil || v.Endpoint.Directory == nil || v.Endpoint.Name == nil || v.Endpoint.Path == nil ||
			*v.Endpoint.Directory != directory || !copySocketName(*v.Endpoint.Name, v.Endpoint.Ordinal) ||
			*v.Endpoint.Path != directory+"/"+*v.Endpoint.Name || len(*v.Endpoint.Path) > 107 {
			return Endpoint{}, invalidBindings()
		}
		endpoint.Path, endpoint.Directory, endpoint.Name = *v.Endpoint.Path, directory, *v.Endpoint.Name
	} else {
		if v.Endpoint.Address == nil || v.Endpoint.Port == nil || v.Endpoint.Directory != nil || v.Endpoint.Name != nil || v.Endpoint.Path != nil ||
			*v.Endpoint.Port < 1024 || *v.Endpoint.Port > 65535 || (endpoint.Scheme != HTTP && endpoint.Scheme != TCP) {
			return Endpoint{}, invalidBindings()
		}
		address, err := netip.ParseAddr(*v.Endpoint.Address)
		if err != nil || (address.String() != "127.0.0.1" && address.String() != "::1") {
			return Endpoint{}, invalidBindings()
		}
		endpoint.Address, endpoint.Port = address, uint16(*v.Endpoint.Port)
	}
	return endpoint, nil
}

func (w featureDescriptor) bindings(runID string) (Bindings, error) {
	rehearsal, err := w.Rehearsal.grant(runID)
	if err != nil {
		return Bindings{}, err
	}
	if !digestIdentifier.MatchString(w.Deployment) || !instanceIdentifier.MatchString(w.Unit.Instance) ||
		!bindingIdentifier.MatchString(w.Unit.Role) || (w.Unit.Ordinal < 1 || w.Unit.Ordinal > maxRoleCopies) || w.Bindings.Schema != 1 ||
		w.Bindings.Role != w.Unit.Role || len(w.Bindings.Executables) > 128 || len(w.Bindings.Endpoints) > 128 || len(w.Bindings.Bundles) > 64 ||
		w.Bindings.Executables == nil || w.Bindings.Endpoints == nil || w.Bindings.Bundles == nil || len(w.Bindings.PublicListeners) > 128 || len(w.Bindings.SharedDirectories) > 32 || len(w.Bindings.Copies) > 128 {
		return Bindings{}, invalidBindings()
	}
	b := Bindings{rehearsal: rehearsal, unit: Unit{InstanceID(w.Unit.Instance), RoleName(w.Unit.Role), uint16(w.Unit.Ordinal)}, deployment: SHA256(w.Deployment),
		executables: make(map[BindingName]Executable), endpoints: make(map[BindingName]Endpoint), bundles: make(map[BindingName]BundleDirectory),
		publicListeners: make(map[BindingName]PublicListener), sharedDirectories: make(map[BindingName]SharedDirectory), copies: make(map[BindingName][]Copy)}
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
		if !unique(v.Name) || !bindingIdentifier.MatchString(v.Endpoint.Role) || !bindingIdentifier.MatchString(v.Endpoint.Listener) {
			return Bindings{}, invalidBindings()
		}
		endpoint, err := resolveEndpoint(v, w.Unit.Instance)
		if err != nil {
			return Bindings{}, err
		}
		b.endpoints[BindingName(v.Name)] = endpoint
	}
	for _, v := range w.Bindings.Copies {
		if !unique(v.Name) || v.Copies == nil || len(v.Copies) > maxRoleCopies {
			return Bindings{}, invalidBindings()
		}
		copies := make([]Copy, 0, len(v.Copies))
		for index, item := range v.Copies {
			ordinal := 1
			if item.Endpoint.Ordinal != nil {
				ordinal = *item.Endpoint.Ordinal
			}
			if item.Ordinal != index+1 || ordinal != item.Ordinal {
				return Bindings{}, invalidBindings()
			}
			endpoint, err := resolveEndpoint(endpointWire{Endpoint: item.Endpoint, Scheme: item.Scheme}, w.Unit.Instance)
			if err != nil {
				return Bindings{}, err
			}
			secret := ""
			if item.SecretFile != nil {
				if err := json.Unmarshal(item.SecretFile, &secret); err != nil {
					return Bindings{}, invalidBindings()
				}
				if !absolutePath(secret) || filepath.Dir(secret) != "/etc/foundry/instances/"+w.Instance+"/copy-secrets" {
					return Bindings{}, invalidBindings()
				}
			}
			if index > 0 && (endpoint.Role != copies[0].Endpoint.Role || endpoint.Listener != copies[0].Endpoint.Listener ||
				endpoint.Scheme != copies[0].Endpoint.Scheme || (secret == "") != (copies[0].SecretFile == "")) {
				return Bindings{}, invalidBindings()
			}
			copies = append(copies, Copy{Ordinal: item.Ordinal, Endpoint: endpoint, SecretFile: secret})
		}
		b.copies[BindingName(v.Name)] = copies
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
		ports := []PublicPort{{Ordinal: 1, Port: uint16(v.Port)}}
		if v.Ports != nil {
			var entries []publicPortWire
			decoder := json.NewDecoder(bytes.NewReader(v.Ports))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&entries); err != nil {
				return Bindings{}, invalidBindings()
			}
			if len(entries) < 2 || len(entries) > maxRoleCopies || entries[0].Port != v.Port {
				return Bindings{}, invalidBindings()
			}
			ports = make([]PublicPort, 0, len(entries))
			seen := make(map[int]bool)
			for index, port := range entries {
				if port.Ordinal != index+1 || port.Port < 1 || port.Port > 65535 || seen[port.Port] {
					return Bindings{}, invalidBindings()
				}
				seen[port.Port] = true
				ports = append(ports, PublicPort{Ordinal: port.Ordinal, Port: uint16(port.Port)})
			}
		}
		b.publicListeners[BindingName(v.Name)] = PublicListener{Role: RoleName(v.Role), Listener: ListenerName(v.Listener),
			Hostname: v.Hostname, Port: uint16(v.Port), Ports: ports, TLS: v.TLS, Transport: v.Transport, PublicAddresses: addresses}
	}
	for _, v := range w.Bindings.SharedDirectories {
		if !unique(v.Name) || v.Path != "/run/foundry/services/"+w.Unit.Instance+"/shared/"+v.Name ||
			(v.Access != DirectoryReadOnly && v.Access != DirectoryReadWrite) ||
			v.MaxBytes < 65536 || v.MaxBytes > 1<<40 || v.MaxBytes%65536 != 0 {
			return Bindings{}, invalidBindings()
		}
		b.sharedDirectories[BindingName(v.Name)] = SharedDirectory{Path: v.Path, Access: v.Access, MaxBytes: v.MaxBytes}
	}
	return b, nil
}
