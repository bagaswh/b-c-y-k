package netiface

import (
	"fmt"
	"net"
)

type IfaceAddresses map[string][]net.Addr

var LocalAddresses IfaceAddresses

func GetLocalAddresses() (IfaceAddresses, error) {
	result := make(IfaceAddresses)
	ifaces, listInterfacesErr := net.Interfaces()
	if listInterfacesErr != nil {
		return IfaceAddresses{}, fmt.Errorf("failed listing interfaces: %v", listInterfacesErr)
	}
	for _, iface := range ifaces {
		addrs, listAddrsErr := iface.Addrs()
		if listAddrsErr != nil {
			return IfaceAddresses{}, fmt.Errorf("failed listing address for iface %s: %v", iface.Name, listAddrsErr)
		}
		for _, addr := range addrs {
			_, ok := result[iface.Name]
			if !ok {
				result[iface.Name] = make([]net.Addr, len(addrs))
			}
			result[iface.Name] = append(result[iface.Name], addr)
		}
	}
	return result, nil
}

func GetLocalAddress(iface string) (net.Addr, error) {
	if len(LocalAddresses) == 0 {
		var getLocalAddressesErr error
		LocalAddresses, getLocalAddressesErr = GetLocalAddresses()
		if getLocalAddressesErr != nil {
			return nil, fmt.Errorf("failed to fetch all local addresses: %v", getLocalAddressesErr)
		}
	}
	localAddr, ok := LocalAddresses[iface]
	if !ok {
		return nil, fmt.Errorf("iface %s is not found", iface)
	}
	return localAddr[len(localAddr)-2], nil
}
