// region/server.go
package region

import (
   "context"
   "net/http"
   
   "github.com/ava-labs/hypersdk/api"
   "github.com/ava-labs/hypersdk/codec"
   "github.com/ava-labs/avalanchego/ids"
)

const (
   Endpoint = "/regionapi"
   Name = "region"
)

type Server struct {
   vm api.VM
}

// GetRegionRequest gets information about a specific region
type GetRegionRequest struct {
   RegionID string `json:"regionId"`
}

type GetRegionResponse struct {
   RegionID string     `json:"regionId"`
   TEEs     []TEEInfo `json:"tees"`
   Status   string    `json:"status"`
}

type TEEInfo struct {
   Address    []byte `json:"address"`
   Status     string `json:"status"`
}

// SubmitRegionalTxRequest submits a transaction to a specific region
type SubmitRegionalTxRequest struct {
   RegionID string       `json:"regionId"`
   TxBytes  codec.Bytes `json:"tx"`
}

type SubmitRegionalTxResponse struct {
   TxID     ids.ID `json:"txId"`
   Status   string `json:"status"`
}

func (s *Server) GetRegion(req *http.Request, args *GetRegionRequest, reply *GetRegionResponse) error {
   ctx, span := s.vm.Tracer().Start(req.Context(), "RegionServer.GetRegion")
   defer span.End()
   
   // Get region info for routing
   region, err := s.vm.State().GetRegion(ctx, args.RegionID)
   if err != nil {
       return err
   }

   reply.RegionID = args.RegionID
   reply.TEEs = region.TEEs
   reply.Status = region.Status
   return nil
}

func (s *Server) SubmitRegionalTx(req *http.Request, args *SubmitRegionalTxRequest, reply *SubmitRegionalTxResponse) error {
   ctx, span := s.vm.Tracer().Start(req.Context(), "RegionServer.SubmitRegionalTx") 
   defer span.End()

   // Route to TEE pair for execution
   results, err := s.routeToTEEPair(ctx, args.RegionID, args.TxBytes)
   if err != nil {
       return err
   }

   reply.TxID = results.TxID
   reply.Status = "executed"
   return nil
}
