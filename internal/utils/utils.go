package utils

import (
	"fmt"
	"net"
)

func IpInCidr(ipAddr, cidr string) (bool, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, err
	}
	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return false, fmt.Errorf("ip is nil")
	}
	return ipNet.Contains(ip), nil
}
