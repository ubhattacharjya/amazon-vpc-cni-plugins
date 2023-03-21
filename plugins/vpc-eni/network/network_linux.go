// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package network

import (
	"github.com/aws/amazon-vpc-cni-plugins/network/imds"
	"github.com/aws/amazon-vpc-cni-plugins/network/netns"
	log "github.com/cihub/seelog"
	"github.com/vishvananda/netlink"
)

// NetBuilder implements the Builder interface for Linux.
type NetBuilder struct{}

// FindOrCreateNetwork creates a new network.
func (nb *NetBuilder) FindOrCreateNetwork(nw *Network) error {
	return nil
}

// DeleteNetwork deletes an existing network.
func (nb *NetBuilder) DeleteNetwork(nw *Network) error {
	return nil
}

// FindOrCreateEndpoint creates a new endpoint in the network.
func (nb *NetBuilder) FindOrCreateEndpoint(nw *Network, ep *Endpoint) error {
	// Find the network namespace.
	log.Infof("Searching for netns %s.", ep.NetNSName)
	ns, err := netns.GetNetNS(ep.NetNSName)
	if err != nil {
		log.Errorf("Failed to find netns %s: %v.", ep.NetNSName, err)
		return err
	}
	log.Infof("Found netns: %v", ns)

	eni := nw.ENI
	err = eni.AttachToLink()
	if err != nil {
		log.Errorf("Failed to find ENI %s: %v", eni, err)
		return err
	}

	log.Infof("Moving ENI link %s to netns %s.", eni, ep.NetNSName)
	err = eni.SetNetNS(ns)
	if err != nil {
		log.Errorf("Failed to move eni: %v.", err)
		return err
	}

	if nw.StayDown {
		// If StayDown flag is set, no need to configure anything else
		return nil
	}

	//Complete the remaining setup in target network namespace.
	err = ns.Run(func() error {
		// Rename the ENI link to the requested interface name.
		if eni.GetLinkName() != ep.ENIName {
			log.Infof("Renaming ENI link %v to %s.", eni, ep.ENIName)
			err := eni.SetLinkName(ep.ENIName)
			if err != nil {
				log.Errorf("Failed to rename ENI link %v: %v.", eni, err)
				return err
			}
		}

		log.Infof("Setting Link MTU %v", ep.MTU)
		if ep.MTU != 0 {
			err = eni.SetLinkMTU(uint(ep.MTU))
			if err != nil {
				log.Errorf("Failed to set mtu for link %v: %v.", eni, err)
				return err
			}
		}

		// Add a blackhole route for IMDS endpoint if required.
		if ep.BlockIMDS {
			err = imds.BlockInstanceMetadataEndpoint()
			if err != nil {
				return err
			}
		}

		// Set ENI IP addresses if specified.
		for _, ipAddress := range ep.IPAddresses {
			// Assign the IP address.
			err = eni.AddIPAddress(&ipAddress)
			if err != nil {
				log.Errorf("Failed to assign IP address to eni %v: %v.", eni, err)
				return err
			}
		}

		log.Infof("Setting ENI link state up.")
		err = eni.SetOpState(true)
		if err != nil {
			log.Errorf("Failed to set link %v state: %v.", eni, err)
			return err
		}

		// Set default gateways if specified.
		for _, gatewayIPAddress := range nw.GatewayIPAddresses {
			// Add default route via ENI link.
			route := &netlink.Route{
				Gw:        gatewayIPAddress,
				LinkIndex: eni.GetLinkIndex(),
			}
			log.Infof("Adding default IP route %+v.", route)
			err = netlink.RouteAdd(route)
			if err != nil {
				log.Errorf("Failed to add IP route %+v via ENI %v: %v.", route, eni, err)
				return err
			}
		}

		return err
	})

	return nil
}

// DeleteEndpoint deletes an existing endpoint.
func (nb *NetBuilder) DeleteEndpoint(nw *Network, ep *Endpoint) error {
	return nil
}
