package region

import (
    "github.com/ava-labs/hypersdk/api"
)

var _ api.HandlerFactory[api.VM] = (*ServerFactory)(nil)

type ServerFactory struct{}

func (ServerFactory) New(vm api.VM) (api.Handler, error) {
    handler, err := api.NewJSONRPCHandler(Name, &Server{vm: vm})
    if err != nil {
        return api.Handler{}, err
    }

    return api.Handler{
        Path:    Endpoint,
        Handler: handler,
    }, nil
}
