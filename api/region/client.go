package region

import (
    "context"
    "strings"

    "github.com/ava-labs/hypersdk/api"
    "github.com/ava-labs/hypersdk/requester" 
)

type Client struct {
    requester *requester.EndpointRequester
}

func NewClient(uri string) *Client {
    uri = strings.TrimSuffix(uri, "/")
    uri += Endpoint
    req := requester.New(uri, Name)
    return &Client{requester: req}
}

func (c *Client) GetRegion(ctx context.Context, regionID string) (*GetRegionResponse, error) {
    resp := new(GetRegionResponse)
    err := c.requester.SendRequest(
        ctx,
        "getRegion",
        &GetRegionRequest{RegionID: regionID},
        resp,
    )
    return resp, err
}

func (c *Client) SubmitRegionalTx(ctx context.Context, regionID string, txBytes []byte) (*SubmitRegionalTxResponse, error) {
    resp := new(SubmitRegionalTxResponse) 
    err := c.requester.SendRequest(
        ctx,
        "submitRegionalTx",
        &SubmitRegionalTxRequest{
            RegionID: regionID,
            TxBytes: txBytes,
        },
        resp,
    )
    return resp, err
}

func (c *Client) VerifyAttestation(ctx context.Context, regionID string, enclaveID []byte, attestation []byte) (*VerifyAttestationResponse, error) {
    resp := new(VerifyAttestationResponse)
    err := c.requester.SendRequest(
        ctx,
        "verifyAttestation", 
        &VerifyAttestationRequest{
            RegionID: regionID,
            EnclaveID: enclaveID,
            Attestation: attestation,
        },
        resp,
    )
    return resp, err
}
