package region

import (
    "github.com/ava-labs/hypersdk/api"
    "github.com/ava-labs/hypersdk/vm"
)

const Namespace = "region"

type Config struct {
    Enabled bool `json:"enabled"`
    // Time window for attestations
    MaxTimeDrift uint64 `json:"maxTimeDrift"`
    // Minimum TEE pairs per region
    MinTEEPairs uint64 `json:"minTeePairs"`
}

func NewDefaultConfig() Config {
    return Config{
        Enabled: true,
        MaxTimeDrift: 300, // 5 minutes in seconds
        MinTEEPairs: 1,   // Minimum one pair per region
    }
}

func With() vm.Option {
    return vm.NewOption(Namespace, NewDefaultConfig(), func(v api.VM, config Config) (vm.Opt, error) {
        if !config.Enabled {
            return vm.NewOpt(), nil
        }
        return vm.WithVMAPIs(ServerFactory{}), nil
    })
}
