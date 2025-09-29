// Copyright 2025 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"net"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const defaultDialServer = "223.5.5.5:53"

func CheckNetworkBlackList(server string, blacklist []string) bool {
	if server == "" {
		server = defaultDialServer
	}

	if len(blacklist) == 0 {
		log.Warn("Blacklist is empty, skipping check")
		return false
	}

	interfacePortable, err := getDefaultInterfacePortable(server)
	if err != nil {
		log.Errorf("Error getting default interface for server %s: %v", server, err)
		return true
	}

	log.Infof("Default network interface for server %s is %s", server, interfacePortable)
	for _, blacklisted := range blacklist {
		if strings.EqualFold(blacklisted, interfacePortable) {
			log.Warnf("Net interface %s is in the blacklist", interfacePortable)
			return true
		}
	}

	return false
}

func getDefaultInterfacePortable(server string) (string, error) {
	if server == "" {
		server = defaultDialServer
	}

	// Note: UDP dial just performs a connect(2), and doesn't actually send a packet.
	c, err := net.Dial("udp4", server)
	if err != nil {
		return "", errors.Wrap(err, "dialing UDP server")
	}

	localAddr, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", errors.New("failed to get UDP local address")
	}
	defer func(c net.Conn) {
		err := c.Close()
		if err != nil {
			log.Errorf("Error closing connection: %s", err)
		}
	}(c)

	ifs, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, ifc := range ifs {
		addrs, err := ifc.Addrs()
		if err != nil {
			return "", err
		}

		for _, addr := range addrs {
			if ipn, ok := addr.(*net.IPNet); ok {
				if ipn.IP.Equal(localAddr.IP) {
					return ifc.Name, nil
				}
			}
		}
	}
	return "", errors.New("no matching network interface found")
}
