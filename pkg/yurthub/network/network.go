/*
Copyright 2021 The OpenYurt Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package network

import (
	"net"
	"time"

	"k8s.io/klog/v2"

	"github.com/openyurtio/openyurt/cmd/yurthub/app/options"
)

const (
	SyncNetworkPeriod = 60
)

type NetworkManager struct {
	ifController DummyInterfaceController
	dummyIfIP    net.IP
	dummyIfName  string
}

func NewNetworkManager(options *options.YurtHubOptions) (*NetworkManager, error) {
	m := &NetworkManager{
		ifController: NewDummyInterfaceController(),
		dummyIfIP:    net.ParseIP(options.HubAgentDummyIfIP),
		dummyIfName:  options.HubAgentDummyIfName,
	}

	if err := m.configureNetwork(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *NetworkManager) Run(stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(SyncNetworkPeriod * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				klog.Infof("exit network manager run goroutine normally")
				err := m.ifController.DeleteDummyInterface(m.dummyIfName)
				if err != nil {
					klog.Errorf("could not delete dummy interface %s, %v", m.dummyIfName, err)
				} else {
					klog.Infof("remove dummy interface %s successfully", m.dummyIfName)
				}
				return
			case <-ticker.C:
				if err := m.configureNetwork(); err != nil {
					// do nothing here
					klog.Warningf("could not configure network, %v", err)
				}
			}
		}
	}()
}

func (m *NetworkManager) configureNetwork() error {
	err := m.ifController.EnsureDummyInterface(m.dummyIfName, m.dummyIfIP)
	if err != nil {
		klog.Errorf("ensure dummy interface failed, %v", err)
		return err
	}

	return nil
}
