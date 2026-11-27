package infra

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ComputeRef is the compute contract's reference (forge 22c): one contract
// for making hosts, so every blueprint above it is the same for every
// provider. A type makes whatever a host needs (Azure's resource group,
// network and address) inside host.create and removes it in host.delete,
// keyed by the host's id.
const (
	ComputeID  = "infra.compute"
	ComputeRef = "infra.compute@1"
)

// Host states a provider reports.
const (
	HostCreating = "creating"
	HostRunning  = "running"
	HostStopped  = "stopped"
	HostDeleting = "deleting"
	HostGone     = "gone"
)

// ProviderHost is a host as a provider reports it.
type ProviderHost struct {
	ID         string         `json:"id"`         // ours: the resource id the host was created with
	ProviderID string         `json:"providerId"` // the provider's own
	Name       string         `json:"name,omitempty"`
	Size       string         `json:"size"`
	Region     string         `json:"region"`
	Image      string         `json:"image,omitempty"`
	State      string         `json:"state"`
	Nested     bool           `json:"nested,omitempty"`
	Addresses  []string       `json:"addresses"`
	Facts      map[string]any `json:"facts,omitempty"`
}

// ProviderSize is a size a provider offers.
type ProviderSize struct {
	ID       string  `json:"id"`
	VCPUs    int     `json:"vcpus"`
	MemoryMB int     `json:"memoryMb"`
	DiskGB   int     `json:"diskGb"`
	Price    float64 `json:"price"` // per hour, in the account's currency
	VMs      bool    `json:"vms"`   // the provider promises nested virtualization
}

const hostSchema = `{"type":"object","description":"A host as the provider reports it","properties":{
  "id":{"type":"string","description":"The host's resource id, as given to host.create"},
  "providerId":{"type":"string","description":"The provider's own id for it"},
  "name":{"type":"string","description":"Its name at the provider"},
  "size":{"type":"string","description":"Its size id (size.list)"},
  "region":{"type":"string","description":"Its region id (region.list)"},
  "image":{"type":"string","description":"The image it was made from"},
  "state":{"type":"string","enum":["creating","running","stopped","deleting","gone"],"description":"Its state"},
  "nested":{"type":"boolean","description":"Whether it can run virtual machines"},
  "addresses":{"type":"array","items":{"type":"string"},"description":"Its IP addresses, public first"},
  "facts":{"type":"object","description":"What else the provider knows, such as its zone"}},"required":["id","providerId","state"]}`

const hostExample = `{"id":"west-general-1","providerId":"vm-0001","name":"west-general-1","size":"m","region":"r1","image":"ubuntu-24.04","state":"creating","addresses":["10.0.0.4"]}`

func hostResult() json.RawMessage { return json.RawMessage(hostSchema) }

func idArgs(what string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The host's resource id` + what + `"}},"required":["id"]}`)
}

// ComputeContract is infra.compute@1.
func ComputeContract() connectors.Contract {
	retryable := []string{connectors.CodeRateLimited, connectors.CodeUnavailable, connectors.CodeConflict}
	return connectors.Contract{
		ID: ComputeID, Version: 1,
		Meta: connectors.Meta{Title: "Compute", Description: "Makes, reads, starts, stops and deletes hosts at a cloud or on-premise provider, and lists its sizes, images and regions. Every provider type implements it, so infrastructure blueprints are the same for every provider. Hosts are named by our resource id, so creating one twice makes one.", Tags: []string{"infrastructure", "compute", "hosts"}},
		Operations: []connectors.Operation{
			{
				ID: "host.create", Title: "Create a host", Description: "Makes a host, and whatever it needs at the provider, under our resource id. A create with an id already made makes nothing and gives the host. It answers at once with the host creating; it becomes running later (host.get, or the host.changed event).",
				Effect: connectors.EffectWrite, Key: "id",
				Args: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"Our resource id for the host, the idempotency key, e.g. west-general-1"},
  "name":{"type":"string","description":"Its name at the provider; the id when left out"},
  "size":{"type":"string","description":"A size id from size.list"},
  "image":{"type":"string","description":"An image id from image.list; the provider's default Ubuntu when left out"},
  "region":{"type":"string","description":"A region id from region.list"},
  "userData":{"type":"string","description":"Cloud-init user data run at first boot"},
  "nested":{"type":"boolean","description":"Whether it must run virtual machines: only sizes with vms are accepted"}},"required":["id","size","region"]}`),
				Result:   hostResult(),
				Examples: []connectors.Example{{Title: "A medium host", Args: json.RawMessage(`{"id":"west-general-1","size":"m","region":"r1","image":"ubuntu-24.04"}`), Result: json.RawMessage(hostExample)}},
				Errors:   append([]string{connectors.CodeInvalid, connectors.CodeAuth}, retryable...),
			},
			{
				ID: "host.get", Title: "Get a host", Description: "Reads one host by our resource id: its state, addresses and facts. notFound when the provider has no such host.",
				Effect: connectors.EffectRead, Args: idArgs(""), Result: hostResult(),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"west-general-1"}`), Result: json.RawMessage(hostExample)}},
				Errors:   append([]string{connectors.CodeNotFound}, retryable...),
			},
			{
				ID: "host.list", Title: "List hosts", Description: "Lists every host this connection made (those with our resource ids), to compare with the configuration.",
				Effect: connectors.EffectRead,
				Args:   json.RawMessage(`{"type":"object","properties":{"region":{"type":"string","description":"Only hosts in this region; every region when left out"}}}`),
				Result: json.RawMessage(`{"type":"object","properties":{"hosts":{"type":"array","items":` + hostSchema + `,"description":"The hosts"}},"required":["hosts"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"hosts":[` + hostExample + `]}`)}},
				Errors:   retryable,
			},
			{
				ID: "host.start", Title: "Start a host", Description: "Starts a stopped host; starting a running one does nothing.",
				Effect: connectors.EffectWrite, Args: idArgs(""), Result: hostResult(),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"west-general-1"}`), Result: json.RawMessage(hostExample)}},
				Errors:   append([]string{connectors.CodeNotFound}, retryable...),
			},
			{
				ID: "host.stop", Title: "Stop a host", Description: "Stops a running host, keeping its disk; stopping a stopped one does nothing.",
				Effect: connectors.EffectWrite, Args: idArgs(""), Result: hostResult(),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"west-general-1"}`), Result: json.RawMessage(`{"id":"west-general-1","providerId":"vm-0001","size":"m","region":"r1","state":"stopped","addresses":[]}`)}},
				Errors:   append([]string{connectors.CodeNotFound}, retryable...),
			},
			{
				ID: "host.delete", Title: "Delete a host", Description: "Deletes a host and what was made for it, with its disk. Deleting a host that is gone succeeds, with deleted false.",
				Effect: connectors.EffectWrite, Destructive: true, Args: idArgs(" to delete"),
				Result:   json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean","description":"Whether there was a host to delete"}},"required":["deleted"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"id":"west-general-1"}`), Result: json.RawMessage(`{"deleted":true}`)}},
				Errors:   retryable,
			},
			{
				ID: "size.list", Title: "List sizes", Description: "The host sizes the provider offers, each with vCPUs, memory, disk, price per hour and vms when it promises nested virtualization.",
				Effect: connectors.EffectRead,
				Args:   json.RawMessage(`{"type":"object","properties":{"region":{"type":"string","description":"Sizes offered in this region; every size when left out"}}}`),
				Result: json.RawMessage(`{"type":"object","properties":{"sizes":{"type":"array","description":"The sizes","items":{"type":"object","properties":{
  "id":{"type":"string","description":"The size's id"},
  "vcpus":{"type":"integer","description":"Virtual CPUs"},
  "memoryMb":{"type":"integer","description":"Memory in MB"},
  "diskGb":{"type":"integer","description":"Disk in GB"},
  "price":{"type":"number","description":"Price per hour in the account's currency"},
  "vms":{"type":"boolean","description":"Whether it can run virtual machines"}},"required":["id"]}}},"required":["sizes"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"sizes":[{"id":"m","vcpus":2,"memoryMb":4096,"diskGb":80,"price":0.04,"vms":false}]}`)}},
				Errors:   retryable,
			},
			{
				ID: "image.list", Title: "List images", Description: "The images hosts can be made from.",
				Effect: connectors.EffectRead,
				Args:   json.RawMessage(`{"type":"object","properties":{}}`),
				Result: json.RawMessage(`{"type":"object","properties":{"images":{"type":"array","description":"The images","items":{"type":"object","properties":{
  "id":{"type":"string","description":"The image's id"},
  "name":{"type":"string","description":"Its name"}},"required":["id"]}}},"required":["images"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"images":[{"id":"ubuntu-24.04","name":"Ubuntu 24.04 LTS"}]}`)}},
				Errors:   retryable,
			},
			{
				ID: "region.list", Title: "List regions", Description: "The regions hosts can be made in.",
				Effect: connectors.EffectRead,
				Args:   json.RawMessage(`{"type":"object","properties":{}}`),
				Result: json.RawMessage(`{"type":"object","properties":{"regions":{"type":"array","description":"The regions","items":{"type":"object","properties":{
  "id":{"type":"string","description":"The region's id"},
  "name":{"type":"string","description":"Its name"}},"required":["id"]}}},"required":["regions"]}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"regions":[{"id":"r1","name":"Region one"}]}`)}},
				Errors:   retryable,
			},
			{
				ID: "check", Title: "Check", Description: "Signs in and lists nothing: whether the connection's credentials work.",
				Effect:   connectors.EffectRead,
				Args:     json.RawMessage(`{"type":"object","properties":{}}`),
				Result:   json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Always true: a failure is an error"}}}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   []string{connectors.CodeAuth, connectors.CodeUnavailable},
			},
		},
		Events: []connectors.EventDef{{
			ID: "host.changed", Title: "Host changed", Description: "A host's state changed at the provider (it became running, stopped or gone). Read the host again rather than trust the order of events; types that cannot watch are polled by the drift job.",
			Payload: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"The event's own id, unique"},
  "host":{"type":"string","description":"The host's resource id"},
  "state":{"type":"string","description":"Its new state"},
  "at":{"type":"string","description":"When, RFC 3339, by the provider's clock"}},"required":["id","host","state"]}`),
			Example:      json.RawMessage(`{"id":"evt_1","host":"west-general-1","state":"running","at":"2026-11-27T10:00:00Z"}`),
			IDFields:     []string{"id"},
			Delivery:     []string{connectors.DeliveryWatch},
			Subscription: "host.changed",
		}},
	}
}

func contributeCompute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, source, []connectors.Contract{ComputeContract()}, nil)
}
