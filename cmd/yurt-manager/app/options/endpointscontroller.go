/*
Copyright 2024 The OpenYurt Authors.
Licensed under the Apache License, Version 2.0 (the License);
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an AS IS BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package options

import (
	"github.com/spf13/pflag"

	"github.com/openyurtio/openyurt/pkg/yurtmanager/controller/servicetopology/endpoints/config"
)

type EndPointsControllerOptions struct {
	*config.ServiceTopologyEndPointsControllerConfiguration
}

func NewEndPointsControllerOptions() *EndPointsControllerOptions {
	return &EndPointsControllerOptions{
		&config.ServiceTopologyEndPointsControllerConfiguration{
			ConcurrentEndPointsWorkers: 3,
		},
	}
}

// AddFlags adds flags related to servicetopology endpoints for yurt-manager to the specified FlagSet.
func (n *EndPointsControllerOptions) AddFlags(fs *pflag.FlagSet) {
	if n == nil {
		return
	}

	fs.Int32Var(&n.ConcurrentEndPointsWorkers, "servicetopology-endpoints-workers", n.ConcurrentEndPointsWorkers, "Max concurrent workers for Servicetopology-endpoints controller.")
}

// ApplyTo fils up servicetopolgy endpoints config with options.
func (o *EndPointsControllerOptions) ApplyTo(cfg *config.ServiceTopologyEndPointsControllerConfiguration) error {
	if o == nil {
		return nil
	}

	cfg.ConcurrentEndPointsWorkers = o.ConcurrentEndPointsWorkers
	return nil
}

// Validate checks validation of EndPointsControllerOptions.
func (o *EndPointsControllerOptions) Validate() []error {
	if o == nil {
		return nil
	}
	errs := []error{}
	return errs
}
