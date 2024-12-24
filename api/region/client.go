// region/config.go
package region

import (
    "github.com/ava-labs/hypersdk/api"
    "github.com/ava-labs/hypersdk/vm"
)

const Namespace = "region"

type Config struct {
    Enabled bool `json:"enabled"`
}

func NewDefaultConfig() Config {
    return Config{
        Enabled: true,
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
