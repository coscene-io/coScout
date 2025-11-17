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
	"crypto/tls"
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

// SecureTLSConfig returns a secure TLS configuration that only allows
// strong cipher suites, rejecting weak CBC-SHA and other insecure suites.
//
// This configuration enforces:
// - TLS version 1.2 only (both MinVersion and MaxVersion set to TLS 1.2)
// - Only GCM-based cipher suites (AES-GCM) for TLS 1.2
// - No CBC-SHA suites (vulnerable to padding oracle attacks)
// - No TLS 1.3 suites (TLS_AES_* and TLS_CHACHA20_POLY1305_SHA256)
//

// Allowed cipher suites for TLS 1.2 (matching server support):
// - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384 (preferred, forward secrecy)
// - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 (preferred, forward secrecy)
// - TLS_RSA_WITH_AES_256_GCM_SHA384 (fallback, no forward secrecy)
// - TLS_RSA_WITH_AES_128_GCM_SHA256 (fallback, no forward secrecy)
//
// TLS 1.3 is disabled by setting MaxVersion to TLS 1.2, ensuring only
// the configured TLS 1.2 cipher suites appear in the Client Hello.
func SecureTLSConfig() *tls.Config {
	return &tls.Config{
		// TLS certificate verification is mandatory for security.
		// InsecureSkipVerify is always false to prevent MITM attacks.
		InsecureSkipVerify: false,
		// Minimum TLS version 1.2 is required for security.
		// TLS 1.0 and 1.1 are deprecated (RFC 8996).
		MinVersion: tls.VersionTLS12,
		// Maximum TLS version 1.2 to match server configuration.
		// This prevents Go from automatically adding TLS 1.3 cipher suites
		// (TLS_AES_128_GCM_SHA256, TLS_AES_256_GCM_SHA384, TLS_CHACHA20_POLY1305_SHA256)
		// in the Client Hello, ensuring only the configured TLS 1.2 suites are used.
		MaxVersion: tls.VersionTLS12,
		// Explicitly allow only secure cipher suites.
		// This prevents the use of weak CBC-SHA suites that are vulnerable
		// to padding oracle attacks (e.g., BEAST, Lucky 13).
		//
		// Only GCM-based suites are allowed:
		// - AES-GCM provides authenticated encryption
		// - No CBC mode (vulnerable to padding attacks)
		// - No CHACHA20_POLY1305 (if policy requires GCM only)
		CipherSuites: []uint16{
			// TLS 1.2 GCM suites (ordered by preference, matching server support)
			// ECDHE suites provide perfect forward secrecy
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, // 0xc030 - A (ecdh_x25519)
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, // 0xc02f - A (ecdh_x25519)
			// RSA suites (fallback, no forward secrecy but still secure)
			// These are required to match server configuration.
			// While RSA key exchange doesn't provide forward secrecy, GCM mode
			// provides authenticated encryption and is still secure.
			//nolint: gosec // G402: RSA key exchange required for server compatibility.
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384, // 0x009d - A (rsa 2048)
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256, // 0x009c - A (rsa 2048)
		},
	}
}
