// SPDX-License-Identifier: Apache-2.0

// Package gcpcontent is the CURATED WIRE SHAPE of every resource this collector
// stores content for, and it is deliberately the only place a content key
// spelling is decided.
//
// WHY A CURATED SHAPE RATHER THAN THE SDK VALUE. Marshaling a provider SDK
// struct straight into a node's content couples the stored graph to whatever
// tags that SDK's code generator happened to emit, and the failure mode is
// silent in both directions. A protobuf-generated Go struct, for one, carries
// the PROTO field name in its encoding/json tag — self_link, network_interfaces,
// nat_i_p — while every hand-written reader in sight is spelled selfLink,
// networkInterfaces and natIP. json.Unmarshal ignores a key it does not know and
// leaves the field at its zero value, so a reader that binds NOTHING and a
// resource that genuinely has no network interfaces are the same empty slice.
//
// So the enumerations WRITE these types and the resolvers READ these same types.
// One declaration, one spelling, and a drift between them is a compile error
// rather than an index that silently populates zero entries.
//
// THAT SAMENESS IS ALSO WHY THIS PACKAGE'S TESTS DO NOT ROUND-TRIP. Encoding a
// value of type T and decoding it back into a T proves nothing about a key
// spelling: writer and reader are the same tag, so a tag that drifts drifts on
// both sides and the assertion still passes. The tests assert the encoded
// document's LITERAL KEYS instead, against a list written out in the test, which
// is the only expectation external to the thing under test.
package gcpcontent

import (
	"encoding/json"
	"fmt"
)

// Marshal encodes a curated content value for storage on a node.
//
// It exists so that "what goes into content" has ONE call site per collector
// rather than forty, and so a converter that reaches for the SDK value instead
// is a visible difference rather than an invisible one.
func Marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding %T as resource content: %w", v, err)
	}
	return raw, nil
}

// Unmarshal decodes a curated content value a resolver reads back.
//
// An empty content is NOT an error: a resource whose converter stored nothing is
// a resource a resolver has no opinion about, and the caller distinguishes that
// from a decode failure by the returned boolean.
func Unmarshal(raw []byte, into any) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, fmt.Errorf("decoding resource content into %T: %w", into, err)
	}
	return true, nil
}

// Instance is the curated content for a Compute Engine instance. The firewall
// index and the DNS address index both read it.
type Instance struct {
	Name              string             `json:"name,omitempty"`
	SelfLink          string             `json:"selfLink,omitempty"`
	Zone              string             `json:"zone,omitempty"`
	MachineType       string             `json:"machineType,omitempty"`
	Status            string             `json:"status,omitempty"`
	CreationTimestamp string             `json:"creationTimestamp,omitempty"`
	Labels            map[string]string  `json:"labels,omitempty"`
	Tags              []string           `json:"tags,omitempty"`
	ServiceAccounts   []string           `json:"serviceAccounts,omitempty"`
	NetworkInterfaces []NetworkInterface `json:"networkInterfaces,omitempty"`
	Disks             []string           `json:"disks,omitempty"`
}

// NetworkInterface is one NIC of an instance.
type NetworkInterface struct {
	Network    string `json:"network,omitempty"`
	Subnetwork string `json:"subnetwork,omitempty"`
	NetworkIP  string `json:"networkIP,omitempty"`
	ExternalIP string `json:"externalIP,omitempty"`
}

// Firewall is the curated content for a VPC firewall rule. The firewall resolver
// reads every field of it, which is why the direction is a plain string and the
// disabled flag a plain bool: an absent key is the zero value, and INGRESS and
// enabled are the API's own defaults.
type Firewall struct {
	Name                  string         `json:"name,omitempty"`
	SelfLink              string         `json:"selfLink,omitempty"`
	Network               string         `json:"network,omitempty"`
	Direction             string         `json:"direction,omitempty"`
	Disabled              bool           `json:"disabled,omitempty"`
	Priority              int32          `json:"priority,omitempty"`
	TargetTags            []string       `json:"targetTags,omitempty"`
	TargetServiceAccounts []string       `json:"targetServiceAccounts,omitempty"`
	SourceRanges          []string       `json:"sourceRanges,omitempty"`
	SourceTags            []string       `json:"sourceTags,omitempty"`
	SourceServiceAccounts []string       `json:"sourceServiceAccounts,omitempty"`
	DestinationRanges     []string       `json:"destinationRanges,omitempty"`
	Allowed               []FirewallRule `json:"allowed,omitempty"`
	Denied                []FirewallRule `json:"denied,omitempty"`
}

// FirewallRule is one protocol-and-ports clause of a firewall rule.
type FirewallRule struct {
	Protocol string   `json:"protocol,omitempty"`
	Ports    []string `json:"ports,omitempty"`
}

// Subnetwork is the curated content for a VPC subnetwork. The Shared VPC
// resolver reads Network, which is the parent VPC's self-link and therefore
// carries the HOST project when the subnet is shared into this one.
type Subnetwork struct {
	Name        string `json:"name,omitempty"`
	SelfLink    string `json:"selfLink,omitempty"`
	Network     string `json:"network,omitempty"`
	Region      string `json:"region,omitempty"`
	IPCIDRRange string `json:"ipCidrRange,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
}

// RunService is the curated content for a Cloud Run service. The image-lineage
// resolver reads the container images out of it.
type RunService struct {
	Name       string   `json:"name,omitempty"`
	URI        string   `json:"uri,omitempty"`
	Images     []string `json:"images,omitempty"`
	Ingress    string   `json:"ingress,omitempty"`
	ServiceAcc string   `json:"serviceAccount,omitempty"`
}

// RecordSet is the curated content for a Cloud DNS record set. The DNS resolver
// reads the type and the rrdatas.
type RecordSet struct {
	Name    string   `json:"name,omitempty"`
	Type    string   `json:"type,omitempty"`
	TTL     int64    `json:"ttl,omitempty"`
	Rrdatas []string `json:"rrdatas,omitempty"`
	Zone    string   `json:"zone,omitempty"`
}

// SQLInstance is the curated content for a Cloud SQL instance. The DNS resolver
// reads the addresses.
type SQLInstance struct {
	Name            string   `json:"name,omitempty"`
	DatabaseVersion string   `json:"databaseVersion,omitempty"`
	Region          string   `json:"region,omitempty"`
	State           string   `json:"state,omitempty"`
	Tier            string   `json:"tier,omitempty"`
	IPAddresses     []string `json:"ipAddresses,omitempty"`
}

// ForwardingRule is the curated content for a compute forwarding rule. The DNS
// resolver reads the address.
type ForwardingRule struct {
	Name       string `json:"name,omitempty"`
	SelfLink   string `json:"selfLink,omitempty"`
	IPAddress  string `json:"ipAddress,omitempty"`
	IPProtocol string `json:"ipProtocol,omitempty"`
	PortRange  string `json:"portRange,omitempty"`
	Target     string `json:"target,omitempty"`
	Network    string `json:"network,omitempty"`
	Subnetwork string `json:"subnetwork,omitempty"`
	Region     string `json:"region,omitempty"`
}

// Generic is the content shape for a resource whose converter has no reader on
// the other side: it preserves the API's own display fields without pretending
// to be a schema anything depends on.
type Generic struct {
	Name        string            `json:"name,omitempty"`
	SelfLink    string            `json:"selfLink,omitempty"`
	Description string            `json:"description,omitempty"`
	State       string            `json:"state,omitempty"`
	Location    string            `json:"location,omitempty"`
	CreateTime  string            `json:"createTime,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
}
